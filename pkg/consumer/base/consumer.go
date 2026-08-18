package base

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer/base/controller"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

// BaseConsumer is built fresh per claimed life, so a respawned runner never
// shares state with a predecessor still draining.
type BaseConsumer[Message any] struct {
	Owner     *common.Owner
	Topic     *topic.Topic
	KeyLeases *controller.KeyLeaseController
	Logger    common.Logger

	abandonedEvents *metricsproducer.MetricsProducer
	consumerFunc    func(ctx context.Context, message *Message) error

	// the two knobs every row's shared machinery paces from -- the runner's
	// own config keeps the rest
	timeoutGrace time.Duration
	ackMargin    time.Duration
}

func NewBaseConsumer[Message any](ctx context.Context, baseDefinition *BaseDefinition[Message], owner *common.Owner, timeoutGrace time.Duration, ackMargin time.Duration) (*BaseConsumer[Message], error) {
	current, err := baseDefinition.topics.GetTopicById(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("%w: topic %d", topic.ErrTopicNotFound, owner.TopicId)
	}

	return &BaseConsumer[Message]{
		Owner:           owner,
		Topic:           current,
		KeyLeases:       baseDefinition.keyLeases,
		Logger:          baseDefinition.Logger,
		abandonedEvents: baseDefinition.abandonedEvents,
		consumerFunc:    baseDefinition.consumerFunc,
		timeoutGrace:    timeoutGrace,
		ackMargin:       ackMargin,
	}, nil
}

// ClaimKeyedRun attempts the exclusive right to run a keyed message; callers
// switch on the returned claim's Verdict. The key must stay held for
// everything the delivery's own lease also covers: the run itself, ctx-cancel
// unwinding, and recording the outcome.
func (b *BaseConsumer[Message]) ClaimKeyedRun(ctx context.Context, key string, messageId int64, resolved *common.MessageOptions) (*controller.KeyLeaseClaim, error) {
	duration := resolved.Timeout + b.timeoutGrace + b.ackMargin
	return b.KeyLeases.ClaimKeyLease(ctx, b.Topic.Id, b.Owner.ConsumerGroupId, key, messageId, duration)
}

// Handles: nil map write, index out of range, bad type assertion
// Does not handle: OS-level fault -- stack overflow, SIGSEGV via cgo, OOM-kill, external kill
func (b *BaseConsumer[Message]) CallSafely(ctx context.Context, payload *Message, messageId int64, attempt int, requested *common.MessageOptions, timeout time.Duration) error {
	// the timeout cause names which side's budget fired
	cause := fmt.Errorf("Timeout (%s) exceeded for message %d attempt %d", timeout, messageId, attempt)
	if requested != nil && requested.Timeout > timeout {
		cause = fmt.Errorf("Timeout (%s) exceeded for message %d attempt %d -- message requested %s, the group ceiling applied", timeout, messageId, attempt, requested.Timeout)
	}

	// work should not be immediately cancelled on a SIGINT/SIGTERM (cancel or shutdown)
	// instead attempt to finish inflight requests bounded by timeout
	ctx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), timeout, cause)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// done is buffered -- always deliverable, even if the select below
				// already gave up on this goroutine via the timeout branch.
				done <- fmt.Errorf("recovered from consumerFunc panic: %v\n%s", r, debug.Stack())
			}
		}()
		done <- b.consumerFunc(ctx, payload)
	}()

	select {
	case err := <-done:
		return err
	// consumerFunc got Timeout to notice ctx and return; past Timeout + grace it
	// is written off and its goroutine is left running, unreachable
	case <-time.After(timeout + b.timeoutGrace):
		b.abandonedEvents.Add(b.Topic.Id, b.Owner.Name, messageId, attempt)
		// done is buffered(1) and nothing else reads it past this point, so this
		// receive fires exactly when the abandoned goroutine finally returns.
		// Started after Add, so Remove can never precede it.
		go func() {
			<-done
			b.abandonedEvents.Remove(b.Topic.Id, b.Owner.Name, messageId, attempt)
		}()

		// never log the message itself -- it may hold sensitive values
		b.Logger.WarnContext(ctx, "consumerFunc hard timeout, goroutine abandoned", "group", b.Owner.Name, "message_id", messageId, "attempt", attempt, "timeout", timeout+b.timeoutGrace)
		return fmt.Errorf("hard timeout after %s, goroutine abandoned for message %d", timeout+b.timeoutGrace, messageId)
	}
}
