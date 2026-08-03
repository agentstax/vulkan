package consumer

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker/waterline"
)

// consumerBase is the plumbing every consumer type composes: the resolved
// topic, datastores, config, the lifecycle gate, and callSafely. Embedded, so
// its exported fields stay part of each type's public surface.
type consumerBase[Message any] struct {
	Topic           *topic.Topic // resolved by Register from the name/version given to the constructor
	Group           *Group       // resolved by Register from consumerGroup -- children are keyed by Group.Id
	Datastore       *ConsumerDatastore[Message]
	AbandonedEvents *consumermetrics.MetricEventProducer
	Config          *ConsumerConfig
	Logger          logger.Logger // copied from Config.Logger at construction

	consumerGroup   string
	topicName       string
	version         topic.SchemaVersion
	topicController *topiccontroller.TopicController
	waterline       *waterline.WaterlineFactory
	lifecycleCtx    context.Context // nil until Register; cancelled = wind down
}

// cfg must already be defaulted and validated by the calling constructor.
func newConsumerBase[Message any](consumerGroup string, topicName string, version topic.SchemaVersion, ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*consumerBase[Message], error) {
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}

	consumerDatastore, err := NewConsumerDatastore[Message](ds, &ConsumerDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	topicController, err := topiccontroller.NewTopicController(ds, &topiccontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	abandonedEvents, err := consumermetrics.NewMetricEventProducer(consumerGroup, ds, &consumermetrics.MetricEventConfig{
		DisableGracefulShutdown: cfg.DisableGracefulShutdown,
		Logger:                  cfg.Logger,
		Retry:                   cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	waterlineFactory, err := waterline.NewWaterlineFactory(ds, &waterline.WaterlineConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &consumerBase[Message]{
		Datastore:       consumerDatastore,
		AbandonedEvents: abandonedEvents,
		Config:          cfg,
		Logger:          cfg.Logger,
		consumerGroup:   consumerGroup,
		topicName:       topicName,
		version:         version,
		topicController: topicController,
		waterline:       waterlineFactory,
	}, nil
}

// register is the bootstrap every consumer type shares: the lifecycle gates,
// topic resolution, schema assert, and cursor upsert. Callers set
// lifecycleCtx themselves, after their own remaining registration steps
// succeed.
func (b *consumerBase[Message]) register(ctx context.Context) error {
	// registration is once per instance
	if b.lifecycleCtx != nil {
		if b.lifecycleCtx.Err() != nil {
			return fmt.Errorf("%w: consumer group %q on topic %q is wound down and stays down; construct a new consumer to consume again", vulkanerrors.ErrAlreadyRegistered, b.consumerGroup, b.Topic.Name)
		}
		return fmt.Errorf("%w: consumer group %q on topic %q -- the context from the first Register still owns this consumer's shutdown", vulkanerrors.ErrAlreadyRegistered, b.consumerGroup, b.Topic.Name)
	}

	// Done() == nil -> context = Background/TODO -> no cancel can ever arrive, so the
	// shutdown phase silently wouldn't exist. Reject unless declared on purpose.
	if ctx.Done() == nil && !b.Config.DisableGracefulShutdown {
		return fmt.Errorf("%w: consumer group %q on topic %q\n%s", vulkanerrors.ErrLifecycleContextNotCancellable, b.consumerGroup, b.topicName, lifecycleContextHelp)
	}

	current, err := b.topicController.GetTopic(ctx, b.topicName, b.version)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: topic %q version %d -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, b.topicName, b.version)
	}
	b.Topic = current

	if err := b.topicController.AssertSchemaSupported(ctx, current.SystemId, current.Id); err != nil {
		return err
	}

	if err := b.AbandonedEvents.Register(ctx); err != nil {
		return err
	}

	group, err := b.Datastore.RegisterGroup(ctx, b.Topic.Id, b.consumerGroup)
	if err != nil {
		return err
	}
	b.Group = group

	return b.seedWaterlineWorker(ctx)
}

// seedWaterlineWorker runs on every register, not just group creation --
// InsertWorker is a no-op when the row exists, so a seed lost to a crash
// heals here.
func (b *consumerBase[Message]) seedWaterlineWorker(ctx context.Context) error {
	owner, err := common.NewConsumerGroupOwner(b.Topic.SystemId, b.Topic.Id, b.Group.Id, b.Group.Name)
	if err != nil {
		return err
	}
	return b.waterline.Seed(ctx, owner)
}

// dispatchVerdict is claimKeyedRun's decision for one keyed message.
type dispatchVerdict string

const (
	dispatchRun        dispatchVerdict = "run"        // key lease acquired -- run now
	dispatchDeferred   dispatchVerdict = "deferred"   // key busy -- the range commit writes its 'deferred' row
	dispatchSuperseded dispatchVerdict = "superseded" // a newer message on the key exists -- never run this one
)

// claimKeyedRun decides whether a keyed message may run right now.
// The returned KeyLeaseClaim is set only on dispatchRun; the caller releases
// it after recording the message's outcome.
func (b *consumerBase[Message]) claimKeyedRun(ctx context.Context, key string, messageID int64, resolved *common.MessageOptions) (dispatchVerdict, *KeyLeaseClaim, error) {
	// same window the range lease pads for: the run itself, ctx-cancel
	// unwinding, and recording the outcome
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

// callSafely catches an in-process Go panic  and turns it into an ordinary error.
// Handles: nil map write, index out of range, bad type assertion
// Does Not Handle: OS-level fault -- stack overflow, SIGSEGV via cgo, OOM-kill, external kill
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
	// hard cutoff for consumerFunc after Timeout + grace (to ideally allow user handling of context timeout instead)
	// if this hard timeout is called go thread will be left hanging / abandoned
	case <-time.After(timeout + b.Config.TimeoutGrace):
		b.AbandonedEvents.Add(ctx, b.Topic.Id, b.consumerGroup, messageID, attempt)
		// reaper -- done is buffered(1) and nothing else reads it past this
		// point, so this receive fires exactly when the abandoned goroutine
		// finally returns. Spawned after Add, so Remove can never precede it.
		go func() {
			<-done
			b.AbandonedEvents.Remove(ctx, b.Topic.Id, b.consumerGroup, messageID, attempt)
		}()

		// don't print out the message in case of sensitive values
		// TODO - documentation should have this known error mesage and how to help prevent it
		// ie handle context.Done or increase TimeoutGrace, we don't want this error to happen often
		// it has bad side effects
		b.Logger.WarnContext(ctx, "consumerFunc hard timeout, goroutine abandoned", "group", b.consumerGroup, "message_id", messageID, "attempt", attempt, "timeout", timeout+b.Config.TimeoutGrace)
		return fmt.Errorf("hard timeout after %s, goroutine abandoned for message %d", timeout+b.Config.TimeoutGrace, messageID)
	}
}

// runCtx merges a call's ctx with the instance lifecycle: whichever cancels
// first stops the loops. A lifecycle cancellation carries ErrShutdownRequested
// as the cause, so exits can tell app shutdown from the caller's own cancel.
func (b *consumerBase[Message]) runCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancelCause(ctx)
	stopWatch := context.AfterFunc(b.lifecycleCtx, func() {
		// if lifecycleCtx is cancelled this is called
		cancel(vulkanerrors.ErrShutdownRequested)
	})

	mergedCancel := func() {
		// if ctx is cancelled this is called
		stopWatch() // unregister AfterFunc (doesn't trigger it)
		cancel(nil) // nil = routine shutdown, ctx.Err() returns context.Canceled
	}

	return merged, mergedCancel
}

// lifecycleErr is the entrypoint gate: loops only start between Register and
// its ctx's cancellation.
func (b *consumerBase[Message]) lifecycleErr() error {
	if b.lifecycleCtx == nil {
		return fmt.Errorf("%w: consumer group %q on topic %q -- call Register with the application's lifetime context before consuming", vulkanerrors.ErrNotRegistered, b.consumerGroup, b.topicName)
	}
	if err := b.lifecycleCtx.Err(); err != nil {
		return fmt.Errorf("%w: consumer group %q on topic %q -- the lifetime context passed to Register is cancelled (%v)", vulkanerrors.ErrShutdownRequested, b.consumerGroup, b.Topic.Name, err)
	}
	return nil
}
