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
	killed, err := r.consumers.Kill(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, r.cfg.MessageMax.Retry.MaxRetries, r.Topic.DeliveryLogMode)
	if err != nil {
		return err
	}
	r.Metrics.RecordDead(int(killed))

	claimed, err := r.consumers.Claim(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, r.cfg.BatchLimit, r.cfg.MessageMax.Retry.MaxRetries, leaseDuration, r.Topic.DeliveryLogMode)
	if err != nil {
		return err
	}
	r.Metrics.RecordClaimed(len(claimed))

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
	if exception.LeaseExpiresAt.Before(time.Now().Add(leaseDuration)) {
		renewed, err := r.consumers.RenewLease(ctx, exception, leaseDuration)
		if err != nil {
			return err
		}
		if !renewed {
			r.Logger.DebugContext(ctx, "lease lost before the run started -- re-claimed by another worker", "group", r.Owner.Name, "topic_id", r.Topic.Id, "message_id", exception.MessageId)
			return nil
		}
	}

	var keyClaim *keyleasecontroller.KeyLeaseClaim
	if exception.MessageKey != "" && resolvedOptions.Concurrency.HoldsKey() {
		claim, err := r.ClaimKeyedRun(ctx, exception.MessageKey, exception.MessageId, exception.Compacted, resolvedOptions)
		switch {
		case err != nil:
			// a failed key-lease claim counts as this attempt's own failure
			return r.recordFailure(ctx, exception, resolvedOptions, err, nil)
		case claim.Verdict == keyleasecontroller.KeyLeaseSuperseded:
			return r.recordSuperseded(ctx, exception)
		case claim.Verdict == keyleasecontroller.KeyLeaseBusy:
			// usually a same-batch sibling on the key started first -- the row
			// returns to 'deferred' and the next claim takes it once the key frees
			r.Logger.DebugContext(ctx, "key busy at gate -- delivery re-deferred", "group", r.Owner.Name, "topic_id", r.Topic.Id, "message_id", exception.MessageId, "message_key", exception.MessageKey)
			return r.recordDeferred(ctx, exception, resolvedOptions.Concurrency)
		}
		keyClaim = claim
	}

	var payload Message
	if err := json.Unmarshal(exception.Payload, &payload); err != nil {
		// bad payload will never deserialize -- no point retrying it
		return r.recordTerminal(ctx, exception, err, keyClaim)
	}

	runCtx := consumergroup.WithMeta(ctx, toExceptionMessageMeta(exception, resolvedOptions))
	err := r.CallSafely(runCtx, &payload, exception.MessageId, exception.Attempts, exception.Options, resolvedOptions.Timeout)

	switch consumerbase.ClassifyHandlerError(err) {
	case consumerbase.HandlerOutcomeTerminal:
		return r.recordTerminal(ctx, exception, err, keyClaim)
	case consumerbase.HandlerOutcomeDelayed:
		return r.recordDelayed(ctx, exception, resolvedOptions, err, keyClaim)
	}
	if err != nil {
		return r.recordFailure(ctx, exception, resolvedOptions, err, keyClaim)
	}

	return r.recordSuccess(ctx, exception, keyClaim)
}

// recordSuccess, recordFailure, recordTerminal, and recordSuperseded mirror
// the cursor path's outcome kinds. A keyed run records on an uncancellable
// ctx: the key release is part of that same transaction and must land even
// mid-shutdown.
func (r *exceptionRunner[Message]) recordSuccess(ctx context.Context, exception *controller.ClaimedException, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	// the run already succeeded -- count it whether or not the record lands
	r.Metrics.RecordSuccess(1)

	recordCtx, cancel := r.recordContext(ctx, keyClaim)
	defer cancel()

	err := r.consumers.RecordSuccess(recordCtx, exception, r.Topic.DeliveryLogMode, keyClaim)
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordFailure(ctx context.Context, exception *controller.ClaimedException, resolvedOptions *common.MessageOptions, runErr error, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	// out of attempts -- this failure is terminal, not another retry
	if exception.Attempts-exception.Delays >= resolvedOptions.Retry.MaxRetries {
		return r.recordTerminal(ctx, exception, runErr, keyClaim)
	}

	recordCtx, cancel := r.recordContext(ctx, keyClaim)
	defer cancel()

	err := r.consumers.RecordFailure(recordCtx, resolvedOptions.Retry, exception, runErr, r.Topic.DeliveryLogMode, keyClaim)
	if err == nil {
		r.Metrics.RecordReady(1)
	}
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordDelayed(ctx context.Context, exception *controller.ClaimedException, resolvedOptions *common.MessageOptions, runErr error, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	// out of delays -- the handler asked for more waiting than the policy allows
	if resolvedOptions.Retry.MaxDelays > 0 && exception.Delays >= resolvedOptions.Retry.MaxDelays {
		return r.recordTerminal(ctx, exception, runErr, keyClaim)
	}

	recordCtx, cancel := r.recordContext(ctx, keyClaim)
	defer cancel()

	delayed, _ := errors.AsType[*consumergroup.DelayedDelivery](runErr)
	err := r.consumers.RecordDelayed(recordCtx, delayed.Delay, exception, runErr, r.Topic.DeliveryLogMode, keyClaim)
	if err == nil {
		r.Metrics.RecordReady(1)
	}
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordTerminal(ctx context.Context, exception *controller.ClaimedException, runErr error, keyClaim *keyleasecontroller.KeyLeaseClaim) error {
	recordCtx, cancel := r.recordContext(ctx, keyClaim)
	defer cancel()

	err := r.consumers.RecordTerminal(recordCtx, exception, runErr, r.Topic.DeliveryLogMode, keyClaim)
	if err == nil {
		r.Metrics.RecordDead(1)
	}
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordSuperseded(ctx context.Context, exception *controller.ClaimedException) error {
	// resolved without a run -- a newer message on the key owns the outcome
	r.Metrics.RecordSuperseded(1)

	err := r.consumers.RecordSuperseded(ctx, exception, r.Topic.DeliveryLogMode)
	return r.absorbLostLease(ctx, exception, err)
}

func (r *exceptionRunner[Message]) recordDeferred(ctx context.Context, exception *controller.ClaimedException, concurrency common.ConcurrencyPolicy) error {
	// no run started -- the row waits out the key's current holder
	r.Metrics.RecordDeferred(1)

	err := r.consumers.RecordDeferred(ctx, exception, concurrency, r.Topic.DeliveryLogMode)
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
		r.Metrics.RecordLeaseLost(1)
		r.Logger.DebugContext(ctx, "lease lost recording exception outcome -- re-claimed by another worker", "group", r.Owner.Name, "topic_id", r.Topic.Id, "message_id", exception.MessageId)
		return nil
	}
	return err
}
