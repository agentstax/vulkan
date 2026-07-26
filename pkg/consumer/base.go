package consumer

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
)

// consumerBase is the plumbing every consumer type composes: the resolved
// topic, datastores, config, the lifecycle gate, and callSafely. Embedded, so
// its exported fields stay part of each type's public surface.
type consumerBase[Message any] struct {
	Topic     *topic.Topic // resolved by Register from the name/version given to the constructor
	Datastore *ConsumerDatastore[Message]
	Metrics   *metrics.ConsumerMetrics // resolved by Register alongside Topic
	Config    *ConsumerConfig
	Logger    logger.Logger // copied from Config.Logger at construction

	consumerGroup  string
	topicName      string
	version        topic.SchemaVersion
	topicDatastore *topic.TopicDatastore
	lifecycleCtx   context.Context // nil until Register; cancelled = wind down
}

// cfg must already be defaulted and validated by the calling constructor.
func newConsumerBase[Message any](consumerGroup string, topicName string, version topic.SchemaVersion, ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*consumerBase[Message], error) {
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}

	consumerDatastore, err := NewConsumerDatastore[Message](ds, &ConsumerDatastoreConfig{
		Logger:       cfg.Logger,
		Retry:        cfg.Retry,
		MessageRetry: cfg.Backoff,
	})
	if err != nil {
		return nil, err
	}

	topicDatastore, err := topic.NewTopicDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &consumerBase[Message]{
		Datastore:      consumerDatastore,
		Config:         cfg,
		Logger:         cfg.Logger,
		consumerGroup:  consumerGroup,
		topicName:      topicName,
		version:        version,
		topicDatastore: topicDatastore,
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

	current, err := b.topicDatastore.GetTopic(ctx, b.topicName, b.version)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: topic %q version %d -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, b.topicName, b.version)
	}
	b.Topic = current

	if err := migrate.AssertSchemaSupported(ctx, b.topicDatastore.Datastore.Pool, current.Id); err != nil {
		return err
	}

	// both consumption paths track the log through this row (CURSOR claims
	// through it, LIFECYCLE records fan-out progress in committed), and it
	// seeds the group's waterline duty
	return b.Datastore.UpsertCursor(ctx, b.Topic.Id, b.consumerGroup)
}

// callSafely catches an in-process Go panic  and turns it into an ordinary error.
// Handles: nil map write, index out of range, bad type assertion
// Does Not Handle: OS-level fault -- stack overflow, SIGSEGV via cgo, OOM-kill, external kill
func (b *consumerBase[Message]) callSafely(ctx context.Context, consumerFunc ConsumerFunc[Message], work *Message, messageID int64, attempt int) error {
	// work should not be immediately cancelled on a SIGINT/SIGTERM (cancel or shutdown)
	// instead attempt to finish inflight requests bounded by timeout
	ctx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), b.Config.WorkTimeout,
		fmt.Errorf("WorkTimeout (%s) exceeded for message %d attempt %d", b.Config.WorkTimeout, messageID, attempt))
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
		done <- consumerFunc(ctx, work)
	}()

	select {
	case err := <-done:
		return err
	// hard cutoff for consumerFunc after WorkTimeout + grace (to ideally allow user handling of context timeout instead)
	// if this hard timeout is called go thread will be left hanging / abandoned
	case <-time.After(b.Config.WorkTimeout + b.Config.WorkTimeoutGrace):
		b.Metrics.AbandonedRoutines.Add(ctx, messageID, attempt)
		// reaper -- done is buffered(1) and nothing else reads it past this
		// point, so this receive fires exactly when the abandoned goroutine
		// finally returns. Spawned after Add, so Remove can never precede it.
		go func() {
			<-done
			b.Metrics.AbandonedRoutines.Remove(ctx, messageID, attempt)
		}()

		// don't print out work in case of sensitive values
		// TODO - documentation should have this known error mesage and how to help prevent it
		// ie handle context.Done or increase WorkTimeoutGrace, we don't want this error to happen often
		// it has bad side effects
		b.Logger.WarnContext(ctx, "consumerFunc hard timeout, goroutine abandoned", "group", b.consumerGroup, "message_id", messageID, "attempt", attempt, "timeout", b.Config.WorkTimeout+b.Config.WorkTimeoutGrace)
		return fmt.Errorf("hard timeout after %s, goroutine abandoned for message %d", b.Config.WorkTimeout+b.Config.WorkTimeoutGrace, messageID)
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
