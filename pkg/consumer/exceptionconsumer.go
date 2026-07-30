package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/topic"
)

// ExceptionConsumer is the bare exception drain: it periodically re-claims
// the group's parked exceptions and retries each through consumerFunc.
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
	leaseDuration := p.Config.MessageMax.WorkTimeout + p.Config.QueueMargin + p.Config.AckMargin

	claimed, err := p.Datastore.ClaimExceptions(ctx, p.Topic.Id, p.Group.Id, p.Config.BatchLimit, p.Config.MessageMax.Retry.MaxRetries, leaseDuration, p.Topic.DisableDeliveryLog)
	if err != nil {
		return err
	}

	for _, exception := range claimed {
		var work Message
		// this payload already deserialized once, in the message loop, to reach
		// the exception window in the first place -- same immutable message_log
		// row, so it cannot fail to unmarshal here. a failure means an invariant
		// broke elsewhere; surface it loudly instead of building unreachable
		// recovery.
		if err := json.Unmarshal(exception.Payload, &work); err != nil {
			return err
		}

		resolvedOptions := p.Config.resolveMessageOptions(exception.Options)
		if err := p.callSafely(withMeta(ctx, exception.toMessageMeta(resolvedOptions)), consumerFunc, &work, exception.MessageId, exception.Attempts, exception.Options, resolvedOptions.WorkTimeout); err != nil {
			if recordErr := p.Datastore.RecordExceptionFailure(ctx, resolvedOptions.Retry, &exception, err, p.Topic.DisableDeliveryLog); recordErr != nil {
				if errors.Is(recordErr, ErrLeaseLost) {
					p.Logger.DebugContext(ctx, "lease lost recording exception failure, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version, "message_id", exception.MessageId)
					continue // reclaimed by the kill backstop or another worker -- not ours anymore
				}
				return recordErr
			}
			continue
		}

		if err := p.Datastore.RecordExceptionSuccess(ctx, &exception); err != nil {
			if errors.Is(err, ErrLeaseLost) {
				p.Logger.DebugContext(ctx, "lease lost recording exception success, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "version", p.version, "message_id", exception.MessageId)
				continue
			}
			return err
		}
	}

	return nil
}
