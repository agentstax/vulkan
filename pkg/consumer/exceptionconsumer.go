package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/topic"
)

// ExceptionConsumer periodically claims the group's retryable 'ready',
// expired 'inflight', and key-freed 'deferred' rows and runs each through
// consumerFunc.
type ExceptionConsumer[Message any] struct {
	*consumerBase[Message]
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewExceptionConsumer[Message any](consumerGroup string, topicName string, version topic.SchemaVersion, ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*ExceptionConsumer[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	base, err := newConsumerBase[Message](consumerGroup, topicName, version, ds, cfg)
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumer[Message]{consumerBase: base}, nil
}

// Register resolves this consumer's topic by name against the live topic row,
// sets up its cursor, and starts the consumer's lifecycle.
//
// ctx must be cancellable, unless ConsumerConfig.DisableGracefulShutdown
// declares otherwise.
func (p *ExceptionConsumer[Message]) Register(ctx context.Context) error {
	if err := p.register(ctx); err != nil {
		return err
	}

	// tracked for graceful shutdown draining / handling
	p.lifecycleCtx = ctx

	return nil
}

// Consume drains the exception window with consumerFunc, blocking until
// stopped: cancel ctx to stop this call, or cancel the context given to
// Register to wind the whole consumer down. A requested stop from either side
// returns nil
func (p *ExceptionConsumer[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	if err := p.lifecycleErr(); err != nil {
		return err
	}
	runCtx, cancel := p.runCtx(ctx)
	defer cancel()

	p.Logger.InfoContext(runCtx, "exception consumer starting", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version)

	err := p.drainExceptions(runCtx, consumerFunc)
	if errors.Is(err, context.Canceled) {
		// requested shutdown (either side), not a failure -- log which side asked
		reason := "caller context cancelled"
		if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
			reason = "lifecycle context cancelled"
		}
		p.Logger.InfoContext(ctx, "exception consumer stopped", "reason", reason, "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version)
		err = nil
	}
	return err
}

func (p *ExceptionConsumer[Message]) drainExceptions(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	ticker := time.NewTicker(p.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.ExceptionClaim(ctx, consumerFunc); err != nil {
				return err
			}
		}
	}
}

func (p *ExceptionConsumer[Message]) ExceptionClaim(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	leaseDuration := p.Config.MessageMax.Timeout + p.Config.TimeoutGrace + p.Config.QueueMargin + p.Config.AckMargin

	// kill first, so an exhausted expired row is dead-lettered
	if err := p.Datastore.KillExceptions(ctx, p.Topic.Id, p.Group.Id, p.Config.MessageMax.Retry.MaxRetries, p.Topic.DisableDeliveryLog); err != nil {
		return err
	}

	claimed, err := p.Datastore.ClaimExceptions(ctx, p.Topic.Id, p.Group.Id, p.Config.BatchLimit, p.Config.MessageMax.Retry.MaxRetries, leaseDuration)
	if err != nil {
		return err
	}

	for i := range claimed {
		if err := p.processException(ctx, &claimed[i], consumerFunc); err != nil {
			return err
		}
	}

	return nil
}

// processException runs consumerFunc for one claimed row and records the outcome.
func (p *ExceptionConsumer[Message]) processException(ctx context.Context, exception *ClaimedException, consumerFunc ConsumerFunc[Message]) error {
	resolvedOptions := p.Config.resolveMessageOptions(exception.Options)

	var keyClaim *KeyLeaseClaim
	if exception.CompactionKey != "" && resolvedOptions.Concurrency == common.ConcurrencyDefer {
		verdict, claim, err := p.claimKeyedRun(ctx, exception.CompactionKey, exception.MessageId, resolvedOptions)
		switch {
		case err != nil:
			// a gate error is this attempt's failure, matching the cursor path
			return p.recordFailure(ctx, exception, resolvedOptions, err, nil)
		case verdict == dispatchSuperseded:
			return p.recordSuperseded(ctx, exception)
		case verdict == dispatchDeferred:
			// this technically should not happen with exception claiming logic
			return fmt.Errorf("key gate returned busy for claimed message %d (key %q): ClaimExceptions must not claim a delivery whose compaction key has an unexpired key_lease", exception.MessageId, exception.CompactionKey)
		}
		keyClaim = claim
	}

	var message Message
	if err := json.Unmarshal(exception.Payload, &message); err != nil {
		// bad payload will never deserialize -- no point retrying it
		return p.recordTerminal(ctx, exception, err, keyClaim)
	}

	if err := p.callSafely(withMeta(ctx, exception.toMessageMeta(resolvedOptions)), consumerFunc, &message, exception.MessageId, exception.Attempts, exception.Options, resolvedOptions.Timeout); err != nil {
		return p.recordFailure(ctx, exception, resolvedOptions, err, keyClaim)
	}
	return p.recordSuccess(ctx, exception, keyClaim)
}

// recordSuccess, recordFailure, recordTerminal, and recordSuperseded mirror
// the buffer's Resolve* verbs. A keyed run records UNCANCELLED -- the key
// release rides the recording transaction and must land even mid-shutdown.
func (p *ExceptionConsumer[Message]) recordSuccess(ctx context.Context, exception *ClaimedException, keyClaim *KeyLeaseClaim) error {
	recordCtx := ctx
	if keyClaim != nil {
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.AckMargin,
			fmt.Errorf("outcome recording exceeded AckMargin (%s) for group %q topic %d", p.Config.AckMargin, p.consumerGroup, p.Topic.Id))
		defer cancel()
	}

	err := p.Datastore.RecordExceptionSuccess(recordCtx, exception, keyClaim)
	if errors.Is(err, ErrLeaseLost) {
		// reclaimed by another worker -- not ours to record anymore
		p.Logger.DebugContext(ctx, "lease lost recording exception outcome, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}

func (p *ExceptionConsumer[Message]) recordFailure(ctx context.Context, exception *ClaimedException, resolvedOptions *common.MessageOptions, runErr error, keyClaim *KeyLeaseClaim) error {
	recordCtx := ctx
	if keyClaim != nil {
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.AckMargin,
			fmt.Errorf("outcome recording exceeded AckMargin (%s) for group %q topic %d", p.Config.AckMargin, p.consumerGroup, p.Topic.Id))
		defer cancel()
	}

	err := p.Datastore.RecordExceptionFailure(recordCtx, resolvedOptions.Retry, exception, runErr, p.Topic.DisableDeliveryLog, keyClaim)
	if errors.Is(err, ErrLeaseLost) {
		p.Logger.DebugContext(ctx, "lease lost recording exception outcome, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}

func (p *ExceptionConsumer[Message]) recordTerminal(ctx context.Context, exception *ClaimedException, runErr error, keyClaim *KeyLeaseClaim) error {
	recordCtx := ctx
	if keyClaim != nil {
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.AckMargin,
			fmt.Errorf("outcome recording exceeded AckMargin (%s) for group %q topic %d", p.Config.AckMargin, p.consumerGroup, p.Topic.Id))
		defer cancel()
	}

	err := p.Datastore.RecordExceptionTerminal(recordCtx, exception, runErr, p.Topic.DisableDeliveryLog, keyClaim)
	if errors.Is(err, ErrLeaseLost) {
		p.Logger.DebugContext(ctx, "lease lost recording exception outcome, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}

func (p *ExceptionConsumer[Message]) recordSuperseded(ctx context.Context, exception *ClaimedException) error {
	err := p.Datastore.RecordExceptionSuperseded(ctx, exception, p.Topic.DisableDeliveryLog)
	if errors.Is(err, ErrLeaseLost) {
		p.Logger.DebugContext(ctx, "lease lost recording exception outcome, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}
