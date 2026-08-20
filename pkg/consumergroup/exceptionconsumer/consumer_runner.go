package exceptionconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumerbase "github.com/agentstax/vulkan/pkg/consumergroup/base"
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumergroup/base/controller"
	"github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer/controller"
)

type exceptionRunner[Message any] struct {
	*consumerbase.BaseConsumer[Message]

	consumers *controller.ExceptionConsumerGroupController
	cfg       *ExceptionConsumerConfig
}

func newExceptionRunner[Message any](base *consumerbase.BaseConsumer[Message], consumers *controller.ExceptionConsumerGroupController, cfg *ExceptionConsumerConfig) (*exceptionRunner[Message], error) {
	if base == nil {
		return nil, errors.New("base must not be nil")
	}
	if consumers == nil {
		return nil, errors.New("consumers controller must not be nil")
	}
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}

	return &exceptionRunner[Message]{BaseConsumer: base, consumers: consumers, cfg: cfg}, nil
}

func (r *exceptionRunner[Message]) run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.exceptionClaim(ctx); err != nil {
				return err
			}
		}
	}
}

func (r *exceptionRunner[Message]) exceptionClaim(ctx context.Context) error {
	leaseDuration := r.cfg.MessageMax.Timeout + r.cfg.TimeoutGrace + r.cfg.QueueMargin + r.cfg.RecordMargin

	// kill first, so an exhausted expired row is dead-lettered
	if err := r.consumers.Kill(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, r.cfg.MessageMax.Retry.MaxRetries, r.Topic.DeliveryLogMode); err != nil {
		return err
	}

	claimed, err := r.consumers.Claim(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, r.cfg.BatchLimit, r.cfg.MessageMax.Retry.MaxRetries, leaseDuration, r.Topic.DeliveryLogMode)
	if err != nil {
		return err
	}

	for i := range claimed {
		if err := r.processException(ctx, &claimed[i]); err != nil {
			return err
		}
	}

	return nil
}

func (r *exceptionRunner[Message]) processException(ctx context.Context, exception *controller.ClaimedException) error {
	resolvedOptions := r.cfg.resolveMessageOptions(exception.Options)

	// sat behind the batch too long for the lease to cover a full run
	// try to renew it rather than start a run the lease can't protect
	leaseDuration := resolvedOptions.Timeout + r.cfg.TimeoutGrace + r.cfg.RecordMargin
	if exception.LeaseUntil.Before(time.Now().Add(leaseDuration)) {
		renewed, err := r.consumers.RenewLease(ctx, exception, leaseDuration)
		if err != nil {
			return err
		}
		if !renewed {
			r.Logger.DebugContext(ctx, "lease lost before the run started, re-claimed by another worker", "group", r.Owner.Name, "topic", r.Topic.Id, "message_id", exception.MessageId)
			return nil
		}
	}

	var keyClaim *keyleasecontroller.KeyLeaseClaim
	if exception.CompactionKey != "" && resolvedOptions.Concurrency == common.ConcurrencyDefer {
		claim, err := r.ClaimKeyedRun(ctx, exception.CompactionKey, exception.MessageId, resolvedOptions)
		switch {
		case err != nil:
			// a failed key-lease claim counts as this attempt's own failure
			return r.recordFailure(ctx, exception, resolvedOptions, err, nil)
		case claim.Verdict == keyleasecontroller.KeyLeaseSuperseded:
			return r.recordSuperseded(ctx, exception)
		case claim.Verdict == keyleasecontroller.KeyLeaseBusy:
			// our lease expired in the batch queue and another worker re-claimed it
			r.Logger.WarnContext(ctx, "key busy at gate, delivery re-claimed by another worker", "group", r.Owner.Name, "topic", r.Topic.Id, "message_id", exception.MessageId, "compaction_key", exception.CompactionKey)
			return nil
		}
		keyClaim = claim
	}

	var payload Message
	if err := json.Unmarshal(exception.Payload, &payload); err != nil {
		// bad payload will never deserialize -- no point retrying it
		return r.recordTerminal(ctx, exception, err, keyClaim)
	}

	runCtx := consumergroup.WithMeta(ctx, toExceptionMessageMeta(exception, resolvedOptions))
	if err := r.CallSafely(runCtx, &payload, exception.MessageId, exception.Attempts, exception.Options, resolvedOptions.Timeout); err != nil {
		return r.recordFailure(ctx, exception, resolvedOptions, err, keyClaim)
	}
	return r.recordSuccess(ctx, exception, keyClaim)
}

// recordSuccess, recordFailure, recordTerminal, and recordSuperseded mirror
// the cursor path's outcome kinds. A keyed run records on an uncancellable
// ctx: the key release is part of that same transaction and must land even
// mid-shutdown.
func (r *exceptionRunner[Message]) recordSuccess(ctx context.Context, exception *controller.ClaimedException, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	recordCtx, cancel := r.recordContext(ctx, keyClaim)
	defer cancel()

	err := r.consumers.RecordSuccess(recordCtx, exception, r.Topic.DeliveryLogMode, keyClaim)
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordFailure(ctx context.Context, exception *controller.ClaimedException, resolvedOptions *common.MessageOptions, runErr error, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	recordCtx, cancel := r.recordContext(ctx, keyClaim)
	defer cancel()

	err := r.consumers.RecordFailure(recordCtx, resolvedOptions.Retry, exception, runErr, r.Topic.DeliveryLogMode, keyClaim)
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordTerminal(ctx context.Context, exception *controller.ClaimedException, runErr error, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	recordCtx, cancel := r.recordContext(ctx, keyClaim)
	defer cancel()

	err := r.consumers.RecordTerminal(recordCtx, exception, runErr, r.Topic.DeliveryLogMode, keyClaim)
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordSuperseded(ctx context.Context, exception *controller.ClaimedException) error {
	err := r.consumers.RecordSuperseded(ctx, exception, r.Topic.DeliveryLogMode)
	return r.absorbLostLease(ctx, exception, err)
}

// a keyed outcome frees the key in the same transaction, so it needs a
// bounded uncancelled window to reach the database mid-shutdown; a keyless
// one rides the caller's ctx.
func (r *exceptionRunner[Message]) recordContext(ctx context.Context, keyClaim *keyleasecontroller.KeyLeaseClaim) (context.Context, context.CancelFunc) {
	if keyClaim == nil {
		return ctx, func() {}
	}
	return context.WithTimeoutCause(context.WithoutCancel(ctx), r.cfg.RecordMargin,
		fmt.Errorf("outcome recording exceeded RecordMargin (%s) for group %q topic %d", r.cfg.RecordMargin, r.Owner.Name, r.Topic.Id))
}

// a lost lease means another worker re-claimed the row -- it owns the outcome
// now, so this side has nothing left to record.
func (r *exceptionRunner[Message]) absorbLostLease(ctx context.Context, exception *controller.ClaimedException, err error) error {
	if errors.Is(err, common.ErrLeaseLost) {
		r.Logger.DebugContext(ctx, "lease lost recording exception outcome, re-claimed by another worker", "group", r.Owner.Name, "topic", r.Topic.Id, "message_id", exception.MessageId)
		return nil
	}
	return err
}
