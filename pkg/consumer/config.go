package consumer

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type ConsumerType string

const (
	CURSOR ConsumerType = "CURSOR"

	// LIFECYCLE is PARKED -- prefer CURSOR. At the current feature set it is a
	// strictly more expensive CURSOR; it re-earns its place only with the
	// non-FIFO queue work (priority/delay/fairness -- see TODO.md).
	LIFECYCLE ConsumerType = "LIFECYCLE"
)

// TODO - better comments for each field. Should follow structure of producer.Options and topic.Config
type MessageConsumerConfig struct {
	Type             ConsumerType
	BatchLimit       int
	FanOutBatchLimit int // max log rows FanOut scans per tick (LIFECYCLE only) -- bounds a cold group's catch-up scan; new messages materialize this many per tick until caught up
	MaxAttempts      int
	MaxRangeReclaims int // past this many reclaims a range is POISON -- quarantined into the exception window instead of handed out again
	ClaimPollRate    time.Duration
	WorkTimeout      time.Duration // bounds consumerFunc itself -- enforced via ctx and the hard-abandon fallback below
	QueueMargin      time.Duration // lease padding for time a claimed item sits queued before a worker starts on it
	AckMargin        time.Duration // lease padding for recording success/failure after consumerFunc returns
	// WorkTimeoutGrace is scheduling slack for a consumerFunc that DID respect
	// ctx.Done() to actually unwind and send on the result channel before the
	// hard cutoff abandons it -- not extra time to keep working. Go's own
	// scheduler wakeup after a context deadline fires is sub-millisecond at p99
	// even under load (measured); this budget is really covering the caller's
	// own cancellation-response time (e.g. a DB driver's cancel-request round
	// trip), which pkg/consumer can't know in general. Default assumes one
	// same-region network round trip's worth of slack.
	WorkTimeoutGrace        time.Duration
	ExceptionInitialBackoff time.Duration // can_run_after delay when an exception/terminal is first parked (Commit/PartialCommit) -- Backoff takes over on later retries
	Backoff                 *retry.Policy // curve for can_run_after on every retry after the first (see ExceptionInitialBackoff). Default: retry.NewDefaultRetryPolicy().
	ShutdownTimeout         time.Duration
	WaterlinePollRate       time.Duration // 0 defaults to ClaimPollRate; set to decouple RollWaterline's tick from the claim loop's -- lower this to shrink committed's staleness (see LEARNING_PLAN.md's Phase 10 "Resolve the lazy-vs-synchronous rollup" for the measured tradeoff against making it synchronous instead)
	Logger                  logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Meter                   metric.Meter
}

func (c *MessageConsumerConfig) WithDefaults() *MessageConsumerConfig {
	if c.Type == "" {
		c.Type = CURSOR
	}
	if c.BatchLimit == 0 {
		c.BatchLimit = 1 // no batching by default
	}
	if c.FanOutBatchLimit == 0 {
		c.FanOutBatchLimit = 1000 // fanout rows are cheap next to processing -- a wide default so only genuinely cold groups feel the cap
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
	if c.QueueMargin == 0 {
		c.QueueMargin = 5 * time.Second
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
	c.Backoff = c.Backoff.WithDefaults()
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 35 * time.Second
	}
	// WaterlinePollRate: zero stays zero -- it already means "use ClaimPollRate" at the use site
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

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MessageConsumerConfig) Validate() error {
	if c.BatchLimit < 1 {
		return fmt.Errorf("BatchLimit must be >= 1, got %d", c.BatchLimit)
	}
	if c.FanOutBatchLimit < 1 {
		return fmt.Errorf("FanOutBatchLimit must be >= 1, got %d", c.FanOutBatchLimit)
	}
	if c.MaxAttempts < 1 {
		return fmt.Errorf("MaxAttempts must be >= 1, got %d", c.MaxAttempts)
	}
	if c.MaxRangeReclaims < 1 {
		return fmt.Errorf("MaxRangeReclaims must be >= 1, got %d", c.MaxRangeReclaims)
	}

	// non-positive durations break their respective loops/timers:
	// ClaimPollRate<=0 -> SleepWithContext/WaitForRoom timers fire immediately (busy loop),
	// WorkTimeout/QueueMargin/AckMargin<=0 -> the lease window math degenerates.
	if c.ClaimPollRate <= 0 {
		return fmt.Errorf("ClaimPollRate must be > 0, got %v", c.ClaimPollRate)
	}
	if c.WorkTimeout <= 0 {
		return fmt.Errorf("WorkTimeout must be > 0, got %v", c.WorkTimeout)
	}
	if c.QueueMargin <= 0 {
		return fmt.Errorf("QueueMargin must be > 0, got %v", c.QueueMargin)
	}
	if c.AckMargin <= 0 {
		return fmt.Errorf("AckMargin must be > 0, got %v", c.AckMargin)
	}
	if c.WorkTimeoutGrace <= 0 {
		return fmt.Errorf("WorkTimeoutGrace must be > 0, got %v", c.WorkTimeoutGrace)
	}
	if c.ExceptionInitialBackoff <= 0 {
		return fmt.Errorf("ExceptionInitialBackoff must be > 0, got %v", c.ExceptionInitialBackoff)
	}
	if c.WaterlinePollRate < 0 {
		return fmt.Errorf("WaterlinePollRate must be >= 0, got %v", c.WaterlinePollRate)
	}

	// shutdown timeout > work timeout + its grace buffer + AckMargin so in-flight
	// work can finish (or get abandoned) AND ack before the pool is torn down
	// (implies ShutdownTimeout > 0 given the guards above)
	if c.ShutdownTimeout <= c.WorkTimeout+c.WorkTimeoutGrace+c.AckMargin {
		return fmt.Errorf("ShutdownTimeout must be > WorkTimeout + WorkTimeoutGrace + AckMargin")
	}

	if err := c.Backoff.Validate(); err != nil {
		return fmt.Errorf("Backoff: %w", err)
	}
	return nil
}

type ConsumerDatastoreConfig struct {
	Logger       logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry        *retry.Policy // transient-error retry policy for this datastore's own Postgres calls. Default: retry.NewDefaultRetryPolicy().
	MessageRetry *retry.Policy // exception/terminal retry-delay curve, unrelated to Retry above. Default: retry.NewDefaultRetryPolicy().
}

func (c *ConsumerDatastoreConfig) WithDefaults() *ConsumerDatastoreConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	c.MessageRetry = c.MessageRetry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ConsumerDatastoreConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	if err := c.MessageRetry.Validate(); err != nil {
		return fmt.Errorf("MessageRetry: %w", err)
	}
	return nil
}
