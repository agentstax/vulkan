package consumer

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// InsertWorker leaves an existing row untouched, so a declaration lost to a crash
// heals on the next Consume. NoInstanceTarget: a consumer's claim gate is the
// caller asking to consume, not a count on the row.
func declareConsumerWorker(ctx context.Context, workers *workercontroller.WorkerController, name string, owner *common.Owner) error {
	if err := workercontroller.ValidateOwner(owner, common.OwnerConsumerGroup, name); err != nil {
		return err
	}
	return workers.InsertWorker(ctx, name, owner, &workercontroller.WorkerConfig{
		TargetInstances: worker.NoInstanceTarget,
	})
}

// consumerBase is built fresh per claimed life, so a respawned runner never
// shares state with a predecessor still draining.
type consumerBase[Message any] struct {
	Owner           *common.Owner
	Topic           *topic.Topic
	Group           *Group
	Datastore       *ConsumerDatastore[Message]
	AbandonedEvents *consumermetrics.MetricEventProducer
	Config          *ConsumerConfig
	Logger          logger.Logger

	consumerGroup string
	version       topic.SchemaVersion
	consumerFunc  ConsumerFunc[Message]
}

func newConsumerBase[Message any](ctx context.Context, ds *datastore.PostgresDatastore, owner *common.Owner, consumerFunc ConsumerFunc[Message], abandonedEvents *consumermetrics.MetricEventProducer, cfg *ConsumerConfig) (*consumerBase[Message], error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if consumerFunc == nil {
		return nil, errors.New("consumerFunc must not be nil")
	}
	if abandonedEvents == nil {
		return nil, errors.New("abandonedEvents producer must not be nil")
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}
	current, err := topicController.GetTopicById(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("%w: topic %d", topic.ErrTopicNotFound, owner.TopicId)
	}

	consumerDatastore, err := NewConsumerDatastore[Message](ds, &ConsumerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &consumerBase[Message]{
		Owner:           owner,
		Topic:           current,
		Group:           &Group{Id: owner.ConsumerGroupId, TopicId: owner.TopicId, Name: owner.Name},
		Datastore:       consumerDatastore,
		AbandonedEvents: abandonedEvents,
		Config:          cfg,
		Logger:          cfg.Logger,
		consumerGroup:   owner.Name,
		version:         current.SchemaVersion,
		consumerFunc:    consumerFunc,
	}, nil
}

// consumePermit is held for the length of a Consume call, so a second Consume
// on the same group is refused rather than running a rival set of runners.
type consumePermit struct {
	owner *common.Owner
	held  atomic.Bool
}

func newConsumePermit(owner *common.Owner) (*consumePermit, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	return &consumePermit{owner: owner}, nil
}

func (p *consumePermit) acquire() (func(), error) {
	if !p.held.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("%w: consumer group %q on topic %d", vulkanerrors.ErrAlreadyConsuming, p.owner.Name, p.owner.TopicId)
	}
	return func() { p.held.Store(false) }, nil
}

// the merged context is done when EITHER ctx or lifecycleCtx cancels. Only a
// lifecycleCtx cancellation carries ErrShutdownRequested as the cause, which is
// how an exit tells app shutdown from the caller's own cancel.
func mergeLifecycle(ctx context.Context, lifecycleCtx context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancelCause(ctx)
	stopWatch := context.AfterFunc(lifecycleCtx, func() {
		cancel(vulkanerrors.ErrShutdownRequested)
	})

	mergedCancel := func() {
		stopWatch() // unregister the AfterFunc (doesn't trigger it)
		cancel(nil) // nil cause = routine shutdown, ctx.Err() returns context.Canceled
	}

	return merged, mergedCancel
}

func stopReason(runCtx context.Context) string {
	if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
		return "lifecycle context cancelled"
	}
	return "caller context cancelled"
}

type dispatchVerdict string

const (
	dispatchRun        dispatchVerdict = "run"        // key lease acquired -- run now
	dispatchDeferred   dispatchVerdict = "deferred"   // key busy -- the range commit writes its 'deferred' row
	dispatchSuperseded dispatchVerdict = "superseded" // a newer message on the key exists -- never run this one
)

// The returned KeyLeaseClaim is set only on dispatchRun; the caller releases
// it after recording the message's outcome.
func (b *consumerBase[Message]) claimKeyedRun(ctx context.Context, key string, messageID int64, resolved *common.MessageOptions) (dispatchVerdict, *KeyLeaseClaim, error) {
	// the key must stay held for everything the range lease also covers: the run
	// itself, ctx-cancel unwinding, and recording the outcome
	duration := resolved.Timeout + b.Config.TimeoutGrace + b.Config.AckMargin

	claim, err := b.Datastore.ClaimKeyLease(ctx, b.Topic.Id, b.Group.Id, key, messageID, duration)
	if err != nil {
		return "", nil, err
	}
	switch claim.Verdict {
	case KeyLeaseAcquired:
		return dispatchRun, claim, nil
	case KeyLeaseSuperseded:
		return dispatchSuperseded, nil, nil
	}

	// busy: another delivery holds the key
	return dispatchDeferred, nil, nil
}

// Handles: nil map write, index out of range, bad type assertion
// Does not handle: OS-level fault -- stack overflow, SIGSEGV via cgo, OOM-kill, external kill
func (b *consumerBase[Message]) callSafely(ctx context.Context, consumerFunc ConsumerFunc[Message], message *Message, messageID int64, attempt int, requested *common.MessageOptions, timeout time.Duration) error {
	// the timeout cause names which side's budget fired
	cause := fmt.Errorf("Timeout (%s) exceeded for message %d attempt %d", timeout, messageID, attempt)
	if requested != nil && requested.Timeout > timeout {
		cause = fmt.Errorf("Timeout (%s) exceeded for message %d attempt %d -- message requested %s, the group ceiling applied", timeout, messageID, attempt, requested.Timeout)
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
		done <- consumerFunc(ctx, message)
	}()

	select {
	case err := <-done:
		return err
	// consumerFunc got Timeout to notice ctx and return; past Timeout + grace it
	// is written off and its goroutine is left running, unreachable
	case <-time.After(timeout + b.Config.TimeoutGrace):
		b.AbandonedEvents.Add(ctx, b.Topic.Id, b.consumerGroup, messageID, attempt)
		// done is buffered(1) and nothing else reads it past this point, so this
		// receive fires exactly when the abandoned goroutine finally returns.
		// Started after Add, so Remove can never precede it.
		go func() {
			<-done
			b.AbandonedEvents.Remove(ctx, b.Topic.Id, b.consumerGroup, messageID, attempt)
		}()

		// never log the message itself -- it may hold sensitive values
		b.Logger.WarnContext(ctx, "consumerFunc hard timeout, goroutine abandoned", "group", b.consumerGroup, "message_id", messageID, "attempt", attempt, "timeout", timeout+b.Config.TimeoutGrace)
		return fmt.Errorf("hard timeout after %s, goroutine abandoned for message %d", timeout+b.Config.TimeoutGrace, messageID)
	}
}
