package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/topic"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/sync/errgroup"
)

// fuck options patterns it always sucks to me
// long live dysfunctional options pattern - https://rednafi.com/go/dysfunctional-options-pattern/

// ideally idepotent func
type ConsumerFunc[Message any] func(ctx context.Context, work *Message) error

type Consumer[Message any] interface {
	Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error
}

type ConsumerType string

const (
	CURSOR    ConsumerType = "CURSOR"
	LIFECYCLE ConsumerType = "LIFECYCLE"
)

// TODO - better comments for each field. Should follow structure of producer.Options and topic.Config
type MessageConsumerConfig struct {
	Type             ConsumerType
	BatchLimit       int
	MaxAttempts      int
	MaxRangeReclaims int // past this many reclaims a range is POISON -- quarantined into the exception window instead of handed out again
	ClaimPollRate    time.Duration
	WorkTimeout      time.Duration // TODO - consider a better name
	QueueTimeout     time.Duration // TODO - consider a better name -- is the time buffer we afford after work to sit in queue
	AckMargin        time.Duration // TODO - consider a better name -- the extra margin of time we give the consumer to record success and failures after consumerFunc processing
	// WorkTimeoutGrace is scheduling slack for a consumerFunc that DID respect
	// ctx.Done() to actually unwind and send on the result channel before the
	// hard cutoff abandons it -- not extra time to keep working. Go's own
	// scheduler wakeup after a context deadline fires is sub-millisecond at p99
	// even under load (measured); this budget is really covering the caller's
	// own cancellation-response time (e.g. a DB driver's cancel-request round
	// trip), which pkg/consumer can't know in general. Default assumes one
	// same-region network round trip's worth of slack.
	WorkTimeoutGrace        time.Duration
	ExceptionInitialBackoff time.Duration // can_run_after delay when an exception/terminal is first parked (Commit/PartialCommit) -- backoff() takes over on later retries
	ShutdownTimeout         time.Duration
	PartitionSafetyBuffer   int64
	JanitorPollRate         time.Duration // 0 defaults to ClaimPollRate; set to decouple the janitor's tick from the claim loop's
	JanitorSweepBatchSize   int           // rows deleted per sweep transaction; caps how much of a backlog one batch holds a lock for
	WaterlinePollRate       time.Duration // 0 defaults to ClaimPollRate; set to decouple RollWaterline's tick from the claim loop's -- lower this to shrink committed's staleness (see LEARNING_PLAN.md's Phase 10 "Resolve the lazy-vs-synchronous rollup" for the measured tradeoff against making it synchronous instead)
	Logger                  logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Meter                   metric.Meter
}

// withDefaults fills every unset (zero) field so a caller can pass a sparse
// config holding only what they care about -- same contract as river's
// Config.WithDefaults. Mutates and returns c for construction-time chaining.
func (c *MessageConsumerConfig) withDefaults() *MessageConsumerConfig {
	if c.Type == "" {
		c.Type = CURSOR
	}
	if c.BatchLimit == 0 {
		c.BatchLimit = 1 // no batching by default
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 3
	}
	if c.MaxRangeReclaims == 0 {
		c.MaxRangeReclaims = 3
	}
	if c.ClaimPollRate == 0 {
		c.ClaimPollRate = 5 * time.Second
	}
	if c.WorkTimeout == 0 {
		c.WorkTimeout = 30 * time.Second
	}
	if c.QueueTimeout == 0 {
		c.QueueTimeout = 5 * time.Second
	}
	if c.AckMargin == 0 {
		c.AckMargin = 2 * time.Second
	}
	if c.WorkTimeoutGrace == 0 {
		c.WorkTimeoutGrace = 100 * time.Millisecond
	}
	if c.ExceptionInitialBackoff == 0 {
		c.ExceptionInitialBackoff = 5 * time.Second
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 35 * time.Second
	}
	if c.PartitionSafetyBuffer == 0 {
		c.PartitionSafetyBuffer = 50000 // TODO - determine sane default
	}
	// JanitorPollRate, WaterlinePollRate: zero stays zero -- it already means "use ClaimPollRate" at the use site
	if c.JanitorSweepBatchSize == 0 {
		c.JanitorSweepBatchSize = 1000
	}
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	if c.Meter == nil {
		// metric/noop, not the global otel.GetMeterProvider() -- reading the
		// global registry requires the top-level otel package, which is
		// bring the trace/baggage/go-logr dependencies ie bloat.
		c.Meter = noop.NewMeterProvider().Meter("github.com/agentstax/vulkan/pkg/consumer")
	}
	return c
}

// TODO - abstract lifecycle funcs like startup -> pull(poll) -> shutdown into a Lifecycle struct with overridable values
type MessageConsumer[Message any] struct {
	Group        string
	Topic        *topic.Topic // the resolved topic.Register return -- id already looked up, never re-resolved per message
	Queue        concurrency.Queue[MessageRow]
	PoolLimiter  concurrency.PoolLimiter
	Datastore    Datastore[Message]
	ShutdownFunc ShutdownFunc[Message]
	Metrics      *metrics.ConsumerMetrics
	Config       *MessageConsumerConfig
	Logger       logger.Logger // copied from Config.Logger at construction
}

// required deps as params, everything else through cfg -- pass nil (or a
// sparse config holding only the fields you care about) and withDefaults
// fills the rest.
func NewMessageConsumer[Message any](group string, t *topic.Topic, queue concurrency.Queue[MessageRow], poolLimiter concurrency.PoolLimiter, ds *datastore.PostgresDatastore, cfg *MessageConsumerConfig) (*MessageConsumer[Message], error) {
	if cfg == nil {
		cfg = &MessageConsumerConfig{}
	}
	cfg.withDefaults()

	consumerDatastore := NewConsumerDatastore[Message](ds, &ConsumerDatastoreConfig{
		Logger: cfg.Logger,
	})

	metrics, err := metrics.NewConsumerMetrics(cfg.Meter, group, t.Id, t.Name, ds, &metrics.ConsumerMetricsDatastoreConfig{
		Logger: cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	return &MessageConsumer[Message]{
		Group:        group,
		Topic:        t,
		Queue:        queue,
		PoolLimiter:  poolLimiter,
		Datastore:    consumerDatastore,
		ShutdownFunc: DefaultShutdownFunc[Message],
		Metrics:      metrics,
		Config:       cfg,
		Logger:       cfg.Logger,
	}, nil
}

// ### CONSUME V2 ###

func (p *MessageConsumer[Message]) validate() error {
	// nil deps first -- everything below dereferences these, so guard before any access
	if p.Config == nil {
		return errors.New("Config must not be nil")
	}
	if p.Topic == nil {
		return errors.New("Topic must not be nil")
	}
	if p.Queue == nil {
		return errors.New("Queue must not be nil")
	}
	if p.PoolLimiter == nil {
		return errors.New("PoolLimiter must not be nil")
	}
	if p.Datastore == nil {
		return errors.New("Datastore must not be nil")
	}
	if p.ShutdownFunc == nil {
		return errors.New("ShutdownFunc must not be nil")
	}
	if p.Metrics == nil {
		return errors.New("Metrics must not be nil")
	}
	if p.Logger == nil {
		return errors.New("Logger must not be nil")
	}

	if p.Config.BatchLimit < 1 {
		return fmt.Errorf("BatchLimit must be >= 1, got %d", p.Config.BatchLimit)
	}

	// Prefetcher can work around this with debounce timeout however
	// having your queue smaller than batch limit seems like a code smell so error for now
	if p.Queue.Cap() < p.Config.BatchLimit {
		return fmt.Errorf("queue cap (%d) must be >= BatchLimit (%d), otherwise prefetcher can never claim a full batch", p.Queue.Cap(), p.Config.BatchLimit)
	}

	if p.Config.MaxAttempts < 1 {
		return fmt.Errorf("MaxAttempts must be >= 1, got %d", p.Config.MaxAttempts)
	}
	if p.Config.MaxRangeReclaims < 1 {
		return fmt.Errorf("MaxRangeReclaims must be >= 1, got %d", p.Config.MaxRangeReclaims)
	}

	// non-positive durations break their respective loops/timers:
	// ClaimPollRate<=0 -> SleepWithContext/WaitForRoom timers fire immediately (busy loop),
	// WorkTimeout/QueueTimeout/AckMargin<=0 -> the lease window math degenerates.
	if p.Config.ClaimPollRate <= 0 {
		return fmt.Errorf("ClaimPollRate must be > 0, got %v", p.Config.ClaimPollRate)
	}
	if p.Config.WorkTimeout <= 0 {
		return fmt.Errorf("WorkTimeout must be > 0, got %v", p.Config.WorkTimeout)
	}
	if p.Config.QueueTimeout <= 0 {
		return fmt.Errorf("QueueTimeout must be > 0, got %v", p.Config.QueueTimeout)
	}
	if p.Config.AckMargin <= 0 {
		return fmt.Errorf("AckMargin must be > 0, got %v", p.Config.AckMargin)
	}
	if p.Config.WorkTimeoutGrace <= 0 {
		return fmt.Errorf("WorkTimeoutGrace must be > 0, got %v", p.Config.WorkTimeoutGrace)
	}
	if p.Config.ExceptionInitialBackoff <= 0 {
		return fmt.Errorf("ExceptionInitialBackoff must be > 0, got %v", p.Config.ExceptionInitialBackoff)
	}
	if p.Config.JanitorPollRate < 0 {
		return fmt.Errorf("JanitorPollRate must be >= 0, got %v", p.Config.JanitorPollRate)
	}
	if p.Config.WaterlinePollRate < 0 {
		return fmt.Errorf("WaterlinePollRate must be >= 0, got %v", p.Config.WaterlinePollRate)
	}
	if p.Config.JanitorSweepBatchSize < 1 {
		return fmt.Errorf("JanitorSweepBatchSize must be >= 1, got %d", p.Config.JanitorSweepBatchSize)
	}

	// shutdown timeout > work timeout + its grace buffer + AckMargin so in-flight
	// work can finish (or get abandoned) AND ack before the pool is torn down
	// (implies ShutdownTimeout > 0 given the guards above)
	if p.Config.ShutdownTimeout <= p.Config.WorkTimeout+p.Config.WorkTimeoutGrace+p.Config.AckMargin {
		return fmt.Errorf("ShutdownTimeout must be > WorkTimeout + WorkTimeoutGrace + AckMargin")
	}

	return nil
}

func (p *MessageConsumer[Message]) Register(ctx context.Context) error {
	// LIFECYCLE groups never read or advance a cursor row (RollWaterline is
	// CURSOR-only) -- creating one anyway would sit at committed=0 forever and
	// wrongly pin the retention floor computed off MIN(committed).
	if p.Config.Type == CURSOR {
		if err := p.Datastore.UpsertCursor(ctx, p.Topic.Id, p.Group); err != nil {
			return err
		}
	}

	// cold-start guarantee: the next partition exists before Janitor's first tick
	if err := p.Datastore.EnsureNextPartition(ctx, p.Topic.Id, p.Topic.PartitionSize, p.Config.PartitionSafetyBuffer); err != nil {
		return err
	}

	return nil
}

// Janitor is the retention/partition-maintenance loop: create-ahead runs every
// tick so a producer never outruns it for long; drop runs alongside it, a
// no-op while RetentionTTL is zero (the default -- retention is opt-in).
func (p *MessageConsumer[Message]) Janitor(ctx context.Context) error {
	janitorPollRate := p.Config.JanitorPollRate
	// TODO - move below default setting this is not the correct place for it
	if janitorPollRate == 0 {
		janitorPollRate = p.Config.ClaimPollRate
	}

	ticker := time.NewTicker(janitorPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.Datastore.EnsureNextPartition(ctx, p.Topic.Id, p.Topic.PartitionSize, p.Config.PartitionSafetyBuffer); err != nil {
				return err
			}
			if err := p.Datastore.DropExpiredPartitions(ctx, p.Topic.Id, p.Topic.PartitionSize, p.Topic.RetentionTTL, p.Topic.AllowDropPastCommitted, p.Topic.DisableDeliveryLog); err != nil {
				return err
			}
			if err := p.Datastore.SweepExpiredPartitions(ctx, p.Topic.Id, p.Topic.PartitionSize, p.Topic.RetentionTTL, p.Topic.AllowDropPastCommitted, p.Config.JanitorSweepBatchSize, p.Topic.DisableDeliveryLog); err != nil {
				return err
			}
			if err := p.Datastore.SweepExpiredIdempotencyKeys(ctx, p.Topic.Id, p.Topic.IdempotencyKeyTTL, p.Config.JanitorSweepBatchSize); err != nil {
				return err
			}
		}
	}
}

func (p *MessageConsumer[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	// fail fast before spawning anything
	// TODO - this should probably be moved to New() -- but requires rethinking dysfunctional options pattern womp womp
	if err := p.validate(); err != nil {
		return err
	}

	errGroup, ctx := errgroup.WithContext(ctx)

	p.Logger.InfoContext(ctx, "consumer deliveries projector starting", "group", p.Group, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.Project(ctx)
	})

	p.Logger.InfoContext(ctx, "consumer starting", "group", p.Group, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.Process(ctx, consumerFunc)
	})

	p.Logger.InfoContext(ctx, "consumer waterline roller starting", "group", p.Group, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.RollWaterline(ctx)
	})

	p.Logger.InfoContext(ctx, "consumer exception drain starting", "group", p.Group, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.DrainExceptions(ctx, consumerFunc)
	})

	p.Logger.InfoContext(ctx, "consumer janitor starting", "group", p.Group, "topic", p.Topic.Id)
	errGroup.Go(func() error {
		return p.Janitor(ctx)
	})

	err := errGroup.Wait()
	if errors.Is(err, context.Canceled) {
		err = nil // requested shutdown, not a failure per say
	}

	// always attempt to gracefully shutdown
	errShutdown := p.Shutdown(ctx)

	// any nil errors are discarded (so both nil -> returns nil)
	return errors.Join(err, errShutdown)
}

func (p *MessageConsumer[Message]) Project(ctx context.Context) error {
	if p.Config.Type == CURSOR {
		return nil // don't need projection for cursor only
	}

	ticker := time.NewTicker(p.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.Datastore.FanOut(ctx, p.Topic.Id, p.Group); err != nil {
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
			if _, err := p.Datastore.AdvanceWaterline(ctx, p.Topic.Id, p.Group); err != nil {
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
	leaseDuration := p.Config.WorkTimeout + p.Config.QueueTimeout + p.Config.AckMargin

	claimed, err := p.Datastore.ClaimMessagesWithCursor(ctx, p.Topic.Id, p.Group, p.Config.BatchLimit, p.Config.MaxRangeReclaims, leaseDuration, p.Topic.DisableDeliveryLog)
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
	if err := p.Datastore.Commit(ctx, p.Topic.Id, p.Group, claimed.Lease.Token, exceptions, terminals, p.Config.ExceptionInitialBackoff, p.Topic.DisableDeliveryLog); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			p.Logger.DebugContext(ctx, "lease lost at commit, ceded range to new owner", "group", p.Group, "topic", p.Topic.Id, "low", claimed.Lease.Low, "high", claimed.Lease.High)
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
		fmt.Errorf("partial commit exceeded AckMargin (%s) for group %q topic %d", p.Config.AckMargin, p.Group, p.Topic.Id))
	defer cancel()

	// narrow the lease to the untouched suffix instead of leaving the WHOLE
	// range (including the already-resolved prefix) to sit out a full reclaim.
	if err := p.Datastore.PartialCommit(commitCtx, p.Topic.Id, p.Group, claimed.Lease.Token, lastProcessed, exceptions, terminals, p.Config.ExceptionInitialBackoff, p.Topic.DisableDeliveryLog); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			p.Logger.DebugContext(ctx, "lease lost at partial commit, ceded range to new owner", "group", p.Group, "topic", p.Topic.Id, "low", claimed.Lease.Low, "high", claimed.Lease.High)
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
	leaseDuration := p.Config.WorkTimeout + p.Config.QueueTimeout + p.Config.AckMargin

	claimed, err := p.Datastore.ClaimExceptions(ctx, p.Topic.Id, p.Group, p.Config.BatchLimit, p.Config.MaxAttempts, leaseDuration, p.Topic.DisableDeliveryLog)
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
					p.Logger.DebugContext(ctx, "lease lost recording exception failure, ceded to new owner", "group", p.Group, "topic", p.Topic.Id, "message_id", exception.MessageId)
					continue // reclaimed by the kill backstop or another worker -- not ours anymore
				}
				return recordErr
			}
			continue
		}

		if err := p.Datastore.RecordExceptionSuccess(ctx, &exception); err != nil {
			if errors.Is(err, ErrLeaseLost) {
				p.Logger.DebugContext(ctx, "lease lost recording exception success, ceded to new owner", "group", p.Group, "topic", p.Topic.Id, "message_id", exception.MessageId)
				continue
			}
			return err
		}
	}

	return nil
}

// LifecycleClaim is the per-row lifecycle path (Phase 6): claim this group's own
// delivery rows and run each through the Phase 2 state machine
// (success -> 'done', retryable failure -> 'ready', exhausted/bad payload -> 'dead').
//
// Unlike CursorClaim, a single message's failure does NOT stop the batch: each
// delivery resolves independently, so group A can dead-letter message 5 while it
// keeps draining 6, 7, 8. That per-message isolation is the whole point of the
// delivery table -- the cursor model can't do it (one bad message blocks the line).
//
// No lease handling here: Phase 6 doesn't do crash recovery, so a delivery left in
// 'processing' (consumer died mid-process) just sits there until Phase 6.5's reclaim.
func (p *MessageConsumer[Message]) LifecycleClaim(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	deliveries, err := p.Datastore.ClaimMessagesWithLifecycle(ctx, p.Topic.Id, p.Group, p.Config.BatchLimit)
	if err != nil {
		return err
	}

	for _, delivery := range deliveries {
		var work Message
		if err := json.Unmarshal(delivery.Payload, &work); err != nil {
			// a bad payload will never deserialize -> straight to the DLQ, no retries
			if recordErr := p.Datastore.RecordTerminal(ctx, &delivery, err, p.Topic.DisableDeliveryLog); recordErr != nil {
				return recordErr
			}
			continue
		}

		if err := p.callSafely(ctx, consumerFunc, &work, delivery.MessageId, delivery.Attempts); err != nil {
			// processing error -> retry until attempts exhaust, then dead-letter
			if recordErr := p.Datastore.RecordFailure(ctx, p.Config.MaxAttempts, &delivery, err, p.Topic.DisableDeliveryLog); recordErr != nil {
				return recordErr
			}
			continue
		}

		if err := p.Datastore.RecordSuccess(ctx, &delivery); err != nil {
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
			// order specific to allow recover to always handle err for consumerFunc not metrics call
			// safe to always call here, if routine was not abandoned it will not be found and skipped
			p.Metrics.AbandonedRoutines.Remove(ctx, messageID, attempt)
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
		// don't print out work in case of sensitive values
		// TODO - documentation should have this known error mesage and how to help prevent it
		// ie handle context.Done or increase WorkTimeoutGrace, we don't want this error to happen often
		// it has bad side effects
		p.Logger.WarnContext(ctx, "consumerFunc hard timeout, goroutine abandoned", "group", p.Group, "message_id", messageID, "attempt", attempt, "timeout", p.Config.WorkTimeout+p.Config.WorkTimeoutGrace)
		return fmt.Errorf("hard timeout after %s, goroutine abandoned for message %d", p.Config.WorkTimeout+p.Config.WorkTimeoutGrace, messageID)
	}
}

func (p *MessageConsumer[Message]) Drain(ctx context.Context, wg *sync.WaitGroup) {
	doneSignal := make(chan struct{})

	go func() {
		wg.Wait()
		close(doneSignal)
	}()

	timer := time.NewTimer(p.Config.ShutdownTimeout)
	defer timer.Stop()

	select {
	case <-doneSignal:
		return // wg has successfully finished ie all in-flight work has finished / drained
	case <-timer.C:
		p.Logger.WarnContext(ctx, "in-flight work did not complete before shutdown timeout, work will be reclaimed after lease expires", "group", p.Group, "topic", p.Topic.Id, "shutdown_timeout", p.Config.ShutdownTimeout)
		return // in-flight work did not finish within timeout, exit early to start shutdown process
	}

}

func (p *MessageConsumer[Message]) Shutdown(ctx context.Context) error {
	// graceful shutdown:
	// - cannot pass cancel context otherwise any functionality that uses ctx will immediately fail which is not what we want
	// - need to pass timeout as well so shutdown cannot hang forever
	shutdownCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.ShutdownTimeout,
		fmt.Errorf("ShutdownTimeout (%s) exceeded for group %q topic %d", p.Config.ShutdownTimeout, p.Group, p.Topic.Id))
	defer cancel()

	return p.ShutdownFunc(shutdownCtx, p)
}
