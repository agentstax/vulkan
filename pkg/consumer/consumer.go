package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	"golang.org/x/sync/errgroup"
)

// ideally idepotent func
type ConsumerFunc[Message any] func(ctx context.Context, work *Message) error

// TODO - abstract lifecycle funcs like startup -> pull(poll) -> shutdown into a Lifecycle struct with overridable values
type MessageConsumer[Message any] struct {
	Topic       *topic.Topic // resolved by Register from the name given to NewMessageConsumer
	Queue       concurrency.Queue[MessageRow]
	PoolLimiter concurrency.PoolLimiter
	Datastore   *ConsumerDatastore[Message]
	Metrics     *metrics.ConsumerMetrics // resolved by Register alongside Topic
	Config      *MessageConsumerConfig
	Logger      logger.Logger // copied from Config.Logger at construction

	consumerGroup  string
	topicName      string
	topicDatastore *topic.TopicDatastore
	lifecycleCtx   context.Context // nil until Register; cancelled = wind down
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageConsumer[Message any](consumerGroup string, topicName string, queue concurrency.Queue[MessageRow], poolLimiter concurrency.PoolLimiter, ds *datastore.PostgresDatastore, cfg *MessageConsumerConfig) (*MessageConsumer[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if queue == nil {
		return nil, errors.New("queue must not be nil")
	}
	if poolLimiter == nil {
		return nil, errors.New("poolLimiter must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &MessageConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Prefetcher can work around this with debounce timeout however
	// having your queue smaller than batch limit seems like a code smell so error for now
	if queue.Cap() < cfg.BatchLimit {
		return nil, fmt.Errorf("queue cap (%d) must be >= BatchLimit (%d), otherwise prefetcher can never claim a full batch", queue.Cap(), cfg.BatchLimit)
	}

	consumerDatastore, err := NewConsumerDatastore[Message](ds, &ConsumerDatastoreConfig{
		Logger:       cfg.Logger,
		MessageRetry: cfg.Backoff,
	})
	if err != nil {
		return nil, err
	}

	topicDatastore, err := topic.NewTopicDatastore(ds, cfg.Logger, nil)
	if err != nil {
		return nil, err
	}

	consumer := &MessageConsumer[Message]{
		consumerGroup:  consumerGroup,
		topicName:      topicName,
		Queue:          queue,
		PoolLimiter:    poolLimiter,
		Datastore:      consumerDatastore,
		Config:         cfg,
		Logger:         cfg.Logger,
		topicDatastore: topicDatastore,
	}

	return consumer, nil
}

// Register resolves this consumer's topic by name against the live topic row,
// sets up its cursor, and starts the consumer's lifecycle.
//
// ctx must be cancellable, unless MessageConsumerConfig.DisableGracefulShutdown
// declares otherwise.
func (p *MessageConsumer[Message]) Register(ctx context.Context) error {
	// registration is once per instance
	if p.lifecycleCtx != nil {
		if p.lifecycleCtx.Err() != nil {
			return fmt.Errorf("%w: consumer group %q on topic %q is wound down and stays down; construct a new MessageConsumer to consume again", vulkanerrors.ErrAlreadyRegistered, p.consumerGroup, p.Topic.Name)
		}
		return fmt.Errorf("%w: consumer group %q on topic %q -- the context from the first Register still owns this consumer's shutdown", vulkanerrors.ErrAlreadyRegistered, p.consumerGroup, p.Topic.Name)
	}

	// Done() == nil -> context = Background/TODO -> no cancel can ever arrive, so the
	// shutdown phase silently wouldn't exist. Reject unless declared on purpose.
	if ctx.Done() == nil && !p.Config.DisableGracefulShutdown {
		return fmt.Errorf("%w: consumer group %q on topic %q\n%s", vulkanerrors.ErrLifecycleContextNotCancellable, p.consumerGroup, p.topicName, lifecycleContextHelp)
	}

	current, err := p.topicDatastore.GetTopic(ctx, p.topicName)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: topic %q -- register it with MessageAdmin.RegisterTopic first", topic.ErrTopicNotFound, p.topicName)
	}
	p.Topic = current

	consumerMetrics, err := metrics.NewConsumerMetrics(p.Config.Meter, p.consumerGroup, current.Id, current.Name, p.Datastore.Datastore, &metrics.ConsumerMetricsDatastoreConfig{
		Logger: p.Config.Logger,
	})
	if err != nil {
		return err
	}
	p.Metrics = consumerMetrics

	// both types track the log through this row:
	//   - CURSOR claims through it
	//   - LIFECYCLE records fan-out progress in committed
	if err := p.Datastore.UpsertCursor(ctx, p.Topic.Id, p.consumerGroup); err != nil {
		return err
	}

	// cold-start guarantee: the next partition exists before Janitor's first tick
	if err := p.Datastore.EnsureNextPartition(ctx, p.Topic.Id, p.Topic.PartitionSize); err != nil {
		return err
	}

	// tracked for graceful shutdown draining / handling
	p.lifecycleCtx = ctx

	return nil
}

// Consume claims and processes messages with consumerFunc, blocking until
// stopped: cancel ctx to stop this call, or cancel the context given to
// Register to wind the whole consumer down. A requested stop from either side
// shuts down in-flight work and returns nil
func (p *MessageConsumer[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	if err := p.lifecycleErr(); err != nil {
		return err
	}
	runCtx, cancel := p.runCtx(ctx)
	defer cancel()

	errGroup, ctx := errgroup.WithContext(runCtx)

	p.Logger.InfoContext(ctx, "consumer deliveries projector starting", "group", p.consumerGroup, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.Project(ctx)
	})

	p.Logger.InfoContext(ctx, "consumer starting", "group", p.consumerGroup, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.Process(ctx, consumerFunc)
	})

	p.Logger.InfoContext(ctx, "consumer waterline roller starting", "group", p.consumerGroup, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.RollWaterline(ctx)
	})

	p.Logger.InfoContext(ctx, "consumer exception drain starting", "group", p.consumerGroup, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.DrainExceptions(ctx, consumerFunc)
	})

	p.Logger.InfoContext(ctx, "consumer janitor starting", "group", p.consumerGroup, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.janitor(ctx)
	})

	err := errGroup.Wait()
	if errors.Is(err, context.Canceled) {
		// requested shutdown (either side), not a failure -- log which side asked
		reason := "caller context cancelled"
		if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
			reason = "lifecycle context cancelled"
		}
		p.Logger.InfoContext(ctx, "consumer stopped", "reason", reason, "group", p.consumerGroup, "topic", p.Topic.Id)
		err = nil
	}

	return err
}

// Janitor runs the retention/partition-maintenance loop standalone, under the
// same rules as Consume: Register first, either ctx or the lifecycle context
// stops it, and a requested stop from either side returns nil.
//
// Public so it can run as its own process or pod: it is topic-scoped, not
// per-group, so one janitor serves every group on the topic -- a dedicated
// maintenance deployment beats N consumers redundantly ticking the same
// drops. Consume also runs this loop internally, so a single-process setup
// needs nothing extra.
func (p *MessageConsumer[Message]) Janitor(ctx context.Context) error {
	if err := p.lifecycleErr(); err != nil {
		return err
	}
	runCtx, cancel := p.runCtx(ctx)
	defer cancel()

	err := p.janitor(runCtx)

	if errors.Is(err, context.Canceled) {
		// requested shutdown (either side), not a failure -- log which side asked
		reason := "caller context cancelled"
		if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
			reason = "lifecycle context cancelled"
		}
		p.Logger.InfoContext(ctx, "janitor stopped", "reason", reason, "topic", p.Topic.Id)
		return nil
	}

	return err
}

// janitor is the retention/partition-maintenance loop: create-ahead runs every
// tick so a producer never outruns it for long; drop runs alongside it, a
// no-op while RetentionTTL is zero (the default -- retention is opt-in).
//
// Topic-scoped, not per-group -- every operation below takes topicID only
func (p *MessageConsumer[Message]) janitor(ctx context.Context) error {
	ticker := time.NewTicker(p.Topic.JanitorPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.Datastore.EnsureNextPartition(ctx, p.Topic.Id, p.Topic.PartitionSize); err != nil {
				return err
			}
			if err := p.Datastore.DropExpiredPartitions(ctx, p.Topic.Id, p.Topic.PartitionSize, p.Topic.RetentionTTL, p.Topic.AllowDropPastCommitted, p.Topic.DisableDeliveryLog); err != nil {
				return err
			}
			if err := p.Datastore.SweepExpiredPartitions(ctx, p.Topic.Id, p.Topic.PartitionSize, p.Topic.RetentionTTL, p.Topic.AllowDropPastCommitted, p.Topic.JanitorSweepBatchSize, p.Topic.DisableDeliveryLog); err != nil {
				return err
			}
			if err := p.Datastore.SweepExpiredIdempotencyKeys(ctx, p.Topic.Id, p.Topic.IdempotencyKeyTTL, p.Topic.JanitorSweepBatchSize); err != nil {
				return err
			}
		}
	}
}

// RollWaterline is the lazy waterline roller: off the hot path, it periodically
// rolls committed up to the lowest open lease (or claimed when none are open). Only
// the cursor path has a waterline; the lifecycle path tracks state per delivery row.
//
// Deliberately lazy, not synchronous-on-Commit -- a synchronous call after
// every Commit adds new lock contention on the shared cursor row. Lower
// WaterlinePollRate instead if committed needs to catch up faster.
func (p *MessageConsumer[Message]) RollWaterline(ctx context.Context) error {
	if p.Config.Type != CURSOR {
		return nil
	}

	// TODO - this is a strange place to set a default, need to reconsider it
	waterlinePollRate := p.Config.WaterlinePollRate
	if waterlinePollRate == 0 {
		waterlinePollRate = p.Config.ClaimPollRate
	}

	ticker := time.NewTicker(waterlinePollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := p.Datastore.AdvanceWaterline(ctx, p.Topic.Id, p.consumerGroup); err != nil {
				return err
			}
		}
	}
}

func (p *MessageConsumer[Message]) Process(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	ticker := time.NewTicker(p.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			switch p.Config.Type {
			case CURSOR:
				if err := p.CursorClaim(ctx, consumerFunc); err != nil {
					return err
				}
			case LIFECYCLE:
				if err := p.LifecycleClaim(ctx, consumerFunc); err != nil {
					return err
				}
			default:
				return errors.New("invalid consumer type")
			}

		}
	}
}

func (p *MessageConsumer[Message]) CursorClaim(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	// leaseDuration should always have extra buffer to not potentially overlap with another worker reclaiming (double processing)
	leaseDuration := p.Config.WorkTimeout + p.Config.QueueMargin + p.Config.AckMargin

	claimed, err := p.Datastore.ClaimMessagesWithCursor(ctx, p.Topic.Id, p.consumerGroup, p.Config.BatchLimit, p.Config.MaxRangeReclaims, leaseDuration, p.Topic.DisableDeliveryLog)
	if err != nil {
		return err
	}
	if claimed == nil {
		return nil // nothing to reclaim or claim -- caught up
	}

	var exceptions []MessageException
	var terminals []MessageTerminal
	lastProcessed := claimed.Lease.Low // partial commit stays at Low if interrupted before any message is reached

	for _, message := range claimed.Messages {
		if ctx.Err() != nil {
			// graceful shutdown mid-range -- stop taking on new messages.
			// everything up to lastProcessed is already resolved below.
			return p.CursorPartialCommit(ctx, lastProcessed, claimed, exceptions, terminals)
		}

		lastProcessed = message.Id

		var work Message
		if err := json.Unmarshal(message.Payload, &work); err != nil {
			// bad payload will never deserialize -- no point retrying it
			terminals = append(terminals, MessageTerminal{MessageId: message.Id, Err: err.Error()})
			continue
		}

		if err := p.callSafely(ctx, consumerFunc, &work, message.Id, 0); err != nil {
			exceptions = append(exceptions, MessageException{MessageId: message.Id, Err: err.Error()})
			continue
		}
	}

	// range always frees -- the lazy roller (RollWaterline) advances committed
	// past it; failures ride along as parked exceptions, not a blocked range.
	if err := p.Datastore.Commit(ctx, p.Topic.Id, p.consumerGroup, claimed.Lease.Token, exceptions, terminals, p.Config.ExceptionInitialBackoff, p.Topic.DisableDeliveryLog); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			p.Logger.DebugContext(ctx, "lease lost at commit, ceded range to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "low", claimed.Lease.Low, "high", claimed.Lease.High)
			return nil // reclaimed mid-range -- the new owner processes it, not a failure here
		}
		return err
	}
	return nil
}

func (p *MessageConsumer[Message]) CursorPartialCommit(ctx context.Context, lastProcessed int64, claimed *ClaimedRange, exceptions []MessageException, terminals []MessageTerminal) error {
	if lastProcessed == claimed.Lease.Low && len(exceptions) == 0 && len(terminals) == 0 {
		return nil // interrupted before resolving anything -- leave the lease exactly as claimed
	}

	// the ctx that got us here is already Done -- the commit needs its own
	// bounded, uncancelled window to actually reach the DB, same as Shutdown
	commitCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.AckMargin,
		fmt.Errorf("partial commit exceeded AckMargin (%s) for group %q topic %d", p.Config.AckMargin, p.consumerGroup, p.Topic.Id))
	defer cancel()

	// narrow the lease to the untouched suffix instead of leaving the WHOLE
	// range (including the already-resolved prefix) to sit out a full reclaim.
	if err := p.Datastore.PartialCommit(commitCtx, p.Topic.Id, p.consumerGroup, claimed.Lease.Token, lastProcessed, exceptions, terminals, p.Config.ExceptionInitialBackoff, p.Topic.DisableDeliveryLog); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			p.Logger.DebugContext(ctx, "lease lost at partial commit, ceded range to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "low", claimed.Lease.Low, "high", claimed.Lease.High)
			return nil // reclaimed mid-range -- the new owner processes it, not a failure here
		}
		// commitCtx expiring mid-call and PartialCommit's own DB error are
		// otherwise indistinguishable from the wire error alone
		if commitCtx.Err() != nil {
			return fmt.Errorf("%w: %w", err, context.Cause(commitCtx))
		}
		return err
	}
	return nil
}

// DrainExceptions is a second poll loop over the sparse exception window, separate
// from CursorClaim's range poll -- a backed-off exception can't block a fresh range
// from claiming, and vice versa. Cursor-path only: the lifecycle path has no waterline
// to pin, so a failed delivery just retries in place on the next LifecycleClaim.
func (p *MessageConsumer[Message]) DrainExceptions(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	if p.Config.Type != CURSOR {
		return nil
	}

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

func (p *MessageConsumer[Message]) ExceptionClaim(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	leaseDuration := p.Config.WorkTimeout + p.Config.QueueMargin + p.Config.AckMargin

	claimed, err := p.Datastore.ClaimExceptions(ctx, p.Topic.Id, p.consumerGroup, p.Config.BatchLimit, p.Config.MaxAttempts, leaseDuration, p.Topic.DisableDeliveryLog)
	if err != nil {
		return err
	}

	for _, exception := range claimed {
		var work Message
		// this payload already deserialized once, in CursorClaim, to reach the
		// exception window in the first place -- same immutable message_log row,
		// so it cannot fail to unmarshal here. a failure means an invariant broke
		// elsewhere; surface it loudly instead of building unreachable recovery.
		if err := json.Unmarshal(exception.Payload, &work); err != nil {
			return err
		}

		if err := p.callSafely(ctx, consumerFunc, &work, exception.MessageId, exception.Attempts); err != nil {
			if recordErr := p.Datastore.RecordExceptionFailure(ctx, p.Config.MaxAttempts, &exception, err, p.Topic.DisableDeliveryLog); recordErr != nil {
				if errors.Is(recordErr, ErrLeaseLost) {
					p.Logger.DebugContext(ctx, "lease lost recording exception failure, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "message_id", exception.MessageId)
					continue // reclaimed by the kill backstop or another worker -- not ours anymore
				}
				return recordErr
			}
			continue
		}

		if err := p.Datastore.RecordExceptionSuccess(ctx, &exception); err != nil {
			if errors.Is(err, ErrLeaseLost) {
				p.Logger.DebugContext(ctx, "lease lost recording exception success, ceded to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "message_id", exception.MessageId)
				continue
			}
			return err
		}
	}

	return nil
}

// callSafely catches an in-process Go panic  and turns it into an ordinary error.
// Handles: nil map write, index out of range, bad type assertion
// Does Not Handle: OS-level fault -- stack overflow, SIGSEGV via cgo, OOM-kill, external kill
func (p *MessageConsumer[Message]) callSafely(ctx context.Context, consumerFunc ConsumerFunc[Message], work *Message, messageID int64, attempt int) error {
	// work should not be immediately cancelled on a SIGINT/SIGTERM (cancel or shutdown)
	// instead attempt to finish inflight requests bounded by timeout
	ctx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.WorkTimeout,
		fmt.Errorf("WorkTimeout (%s) exceeded for message %d attempt %d", p.Config.WorkTimeout, messageID, attempt))
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
	case <-time.After(p.Config.WorkTimeout + p.Config.WorkTimeoutGrace):
		p.Metrics.AbandonedRoutines.Add(ctx, messageID, attempt)
		// reaper -- done is buffered(1) and nothing else reads it past this
		// point, so this receive fires exactly when the abandoned goroutine
		// finally returns. Spawned after Add, so Remove can never precede it.
		go func() {
			<-done
			p.Metrics.AbandonedRoutines.Remove(ctx, messageID, attempt)
		}()

		// don't print out work in case of sensitive values
		// TODO - documentation should have this known error mesage and how to help prevent it
		// ie handle context.Done or increase WorkTimeoutGrace, we don't want this error to happen often
		// it has bad side effects
		p.Logger.WarnContext(ctx, "consumerFunc hard timeout, goroutine abandoned", "group", p.consumerGroup, "message_id", messageID, "attempt", attempt, "timeout", p.Config.WorkTimeout+p.Config.WorkTimeoutGrace)
		return fmt.Errorf("hard timeout after %s, goroutine abandoned for message %d", p.Config.WorkTimeout+p.Config.WorkTimeoutGrace, messageID)
	}
}

// runCtx merges a call's ctx with the instance lifecycle: whichever cancels
// first stops the loops. A lifecycle cancellation carries ErrShutdownRequested
// as the cause, so exits can tell app shutdown from the caller's own cancel.
func (p *MessageConsumer[Message]) runCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancelCause(ctx)
	stopWatch := context.AfterFunc(p.lifecycleCtx, func() {
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
func (p *MessageConsumer[Message]) lifecycleErr() error {
	if p.lifecycleCtx == nil {
		return fmt.Errorf("%w: consumer group %q on topic %q -- call Register with the application's lifetime context before consuming", vulkanerrors.ErrNotRegistered, p.consumerGroup, p.topicName)
	}
	if err := p.lifecycleCtx.Err(); err != nil {
		return fmt.Errorf("%w: consumer group %q on topic %q -- the lifetime context passed to Register is cancelled (%v)", vulkanerrors.ErrShutdownRequested, p.consumerGroup, p.Topic.Name, err)
	}
	return nil
}
