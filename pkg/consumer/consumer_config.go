package consumer

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

type ConsumerType string

const (
	CURSOR ConsumerType = "CURSOR"

	// LIFECYCLE is ON HOLD -- prefer CURSOR. At the current feature set it is a
	// strictly more expensive CURSOR; it re-earns its place only with the
	// non-FIFO queue work (priority/delay/fairness).
	LIFECYCLE ConsumerType = "LIFECYCLE"
)

// TODO - better comments for each field. Should follow structure of producer.Options and the topic register Config
type ConsumerConfig struct {
	Type               ConsumerType
	BatchLimit         int
	QueueSize          int // claimed messages buffered ahead of processing (CURSOR only) -- must be >= BatchLimit or the prefetcher can never claim a full batch. Default: BatchLimit.
	MessageConcurrency int // messages processed concurrently (CURSOR only). Default: 1.
	FanOutBatchLimit   int // max log rows FanOut scans per tick (LIFECYCLE only) -- bounds a cold group's catch-up scan; new messages materialize this many per tick until caught up
	MaxRangeReclaims   int // past this many reclaims a range is POISON -- quarantined into the exception window instead of handed out again
	ClaimPollRate      time.Duration
	QueueMargin        time.Duration // lease padding for time a claimed item sits queued before a worker starts on it
	RecordMargin       time.Duration // lease padding for recording success/failure after consumerFunc returns
	// TimeoutGrace is scheduling slack for a consumerFunc that DID respect
	// ctx.Done() to actually unwind and send on the result channel before the
	// hard cutoff abandons it -- not extra time to keep working. Go's own
	// scheduler wakeup after a context deadline fires is sub-millisecond at p99
	// even under load (measured); this budget is really covering the caller's
	// own cancellation-response time (e.g. a DB driver's cancel-request round
	// trip), which pkg/consumer can't know in general. Default assumes one
	// same-region network round trip's worth of slack.
	TimeoutGrace            time.Duration
	ExceptionInitialBackoff time.Duration       // can_run_after delay when an exception/terminal row is first written (Commit/PartialCommit) -- Message.Retry takes over on later retries
	InstanceTTL             time.Duration       // how long this consumer's claimed worker_instance rows stay live without a heartbeat renewal -- past it a replacement can claim. Default: 30s.
	BindingRetryInterval    time.Duration       // how often Consume re-attempts a waiting binding declaration while a live instance still declares a different set. Default: 10s.
	Retry                   *common.RetryPolicy // transient-error retry policy for this consumer's own Postgres calls -- never applies to message redelivery, that is Message.Retry. Default: common.NewDefaultRetryPolicy().
	ShutdownTimeout         time.Duration       // bounds how long drain waits for in-flight processClaim calls to finish before closeOpenRanges settles whatever's left. Default: MessageMax.Timeout + TimeoutGrace + RecordMargin -- one callSafely's worst case at the ceiling a message may request, plus recording its outcome
	Logger                  common.Logger       // pass your own *slog.Logger (own Handler) or anything satisfying common.Logger. Default: text logger to stdout, warn level and up.

	// Message - default MessageOptions: fills any option the produced message left unset.
	// Default: Timeout 30s; Retry MaxRetries 3 with the default curve.
	Message *common.MessageOptions

	// MessageMin - per-option floors: raises any resolved option below these.
	// Concurrency is not orderable and must stay unset.
	// Default: nil (no floors).
	MessageMin *common.MessageOptions

	// MessageMax - per-option ceilings: lowers any resolved option above these.
	// Concurrency is not orderable and must stay unset.
	// Default: Message's values -- messages cannot request above the group's
	// defaults unless raised here.
	MessageMax *common.MessageOptions

	// ConcurrencyOverride - this group runs every message under this policy,
	// beating whatever the message requested.
	// Default: "" (honor each message's own policy).
	ConcurrencyOverride common.ConcurrencyPolicy

	// DisableGracefulShutdown - lets Consume accept a context that can never
	// be cancelled (e.g. context.Background()), leaving process exit as the
	// only stop. Prefer passing the application's shutdown context to Consume.
	// Default: false.
	DisableGracefulShutdown bool
}

func (c *ConsumerConfig) WithDefaults() *ConsumerConfig {
	if c.Type == "" {
		c.Type = CURSOR
	}

	if c.BatchLimit == 0 {
		c.BatchLimit = 1 // no batching by default
	}

	if c.QueueSize == 0 {
		c.QueueSize = c.BatchLimit
	}

	if c.MessageConcurrency == 0 {
		c.MessageConcurrency = 1
	}

	if c.FanOutBatchLimit == 0 {
		c.FanOutBatchLimit = 1000 // fanout rows are cheap next to processing -- a wide default so only genuinely cold groups feel the cap
	}

	if c.MaxRangeReclaims == 0 {
		c.MaxRangeReclaims = 3
	}

	if c.ClaimPollRate == 0 {
		c.ClaimPollRate = 5 * time.Second
	}

	if c.QueueMargin == 0 {
		c.QueueMargin = 5 * time.Second
	}

	if c.RecordMargin == 0 {
		c.RecordMargin = 2 * time.Second
	}

	if c.TimeoutGrace == 0 {
		c.TimeoutGrace = 100 * time.Millisecond
	}

	if c.ExceptionInitialBackoff == 0 {
		c.ExceptionInitialBackoff = 5 * time.Second
	}

	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}

	if c.BindingRetryInterval == 0 {
		c.BindingRetryInterval = 10 * time.Second
	}

	c.Message = c.Message.WithDefaults()

	// ceilings must always exist -- lease sizing and the kill backstop need them
	bounds := *c.Message
	bounds.Concurrency = "" // should never be set for a 'bound'
	c.MessageMax = c.MessageMax.Fill(&bounds)

	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = c.MessageMax.Timeout + c.TimeoutGrace + c.RecordMargin
	}

	if c.Retry == nil {
		c.Retry = &common.RetryPolicy{}
	}
	c.Retry = c.Retry.WithDefaults()

	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stdout)
	}

	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ConsumerConfig) Validate() error {
	if c.BatchLimit < 1 {
		return fmt.Errorf("BatchLimit must be >= 1, got %d", c.BatchLimit)
	}
	if c.QueueSize < c.BatchLimit {
		return fmt.Errorf("QueueSize (%d) must be >= BatchLimit (%d), otherwise the prefetcher can never claim a full batch", c.QueueSize, c.BatchLimit)
	}
	if c.MessageConcurrency < 1 {
		return fmt.Errorf("MessageConcurrency must be >= 1, got %d", c.MessageConcurrency)
	}
	if c.FanOutBatchLimit < 1 {
		return fmt.Errorf("FanOutBatchLimit must be >= 1, got %d", c.FanOutBatchLimit)
	}
	if c.MaxRangeReclaims < 1 {
		return fmt.Errorf("MaxRangeReclaims must be >= 1, got %d", c.MaxRangeReclaims)
	}

	// non-positive durations break their respective loops/timers:
	// ClaimPollRate<=0 -> the prefetch back-off and WaitForRoom timers fire immediately (busy loop),
	// Message.Timeout/QueueMargin/RecordMargin<=0 -> the lease window math degenerates.
	if c.ClaimPollRate <= 0 {
		return fmt.Errorf("ClaimPollRate must be > 0, got %v", c.ClaimPollRate)
	}
	if c.Message.Timeout <= 0 {
		return fmt.Errorf("Message.Timeout must be > 0, got %v", c.Message.Timeout)
	}
	if c.QueueMargin <= 0 {
		return fmt.Errorf("QueueMargin must be > 0, got %v", c.QueueMargin)
	}
	if c.RecordMargin <= 0 {
		return fmt.Errorf("RecordMargin must be > 0, got %v", c.RecordMargin)
	}
	if c.TimeoutGrace <= 0 {
		return fmt.Errorf("TimeoutGrace must be > 0, got %v", c.TimeoutGrace)
	}
	if c.ExceptionInitialBackoff <= 0 {
		return fmt.Errorf("ExceptionInitialBackoff must be > 0, got %v", c.ExceptionInitialBackoff)
	}
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	if c.BindingRetryInterval <= 0 {
		return fmt.Errorf("BindingRetryInterval must be > 0, got %v", c.BindingRetryInterval)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("ShutdownTimeout must be > 0, got %v", c.ShutdownTimeout)
	}

	if err := c.Message.Retry.Validate(); err != nil {
		return fmt.Errorf("Message.Retry: %w", err)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}

	if err := c.MessageMin.Validate(); err != nil {
		return fmt.Errorf("MessageMin: %w", err)
	}
	if err := c.MessageMax.Validate(); err != nil {
		return fmt.Errorf("MessageMax: %w", err)
	}
	if c.MessageMin != nil && c.MessageMin.Concurrency != "" {
		return fmt.Errorf("MessageMin must not set Concurrency -- a policy is not orderable, got %q", c.MessageMin.Concurrency)
	}
	if c.MessageMax.Concurrency != "" {
		return fmt.Errorf("MessageMax must not set Concurrency -- a policy is not orderable, got %q", c.MessageMax.Concurrency)
	}
	if err := c.ConcurrencyOverride.Validate(); err != nil {
		return fmt.Errorf("ConcurrencyOverride: %w", err)
	}
	return c.validateMessageBounds()
}

// enforces MessageMin <= Message <= MessageMax per field, so every resolved
// option lands inside the bounds
func (c *ConsumerConfig) validateMessageBounds() error {
	if c.MessageMin != nil {
		if c.MessageMin.Timeout > c.Message.Timeout {
			return fmt.Errorf("MessageMin.Timeout (%v) must be <= Message.Timeout (%v)", c.MessageMin.Timeout, c.Message.Timeout)
		}
		if r := c.MessageMin.Retry; r != nil {
			if r.MaxRetries > c.Message.Retry.MaxRetries {
				return fmt.Errorf("MessageMin.Retry.MaxRetries (%d) must be <= Message.Retry.MaxRetries (%d)", r.MaxRetries, c.Message.Retry.MaxRetries)
			}
			if r.BaseDelay > c.Message.Retry.BaseDelay {
				return fmt.Errorf("MessageMin.Retry.BaseDelay (%v) must be <= Message.Retry.BaseDelay (%v)", r.BaseDelay, c.Message.Retry.BaseDelay)
			}
			if r.MaxDelay > c.Message.Retry.MaxDelay {
				return fmt.Errorf("MessageMin.Retry.MaxDelay (%v) must be <= Message.Retry.MaxDelay (%v)", r.MaxDelay, c.Message.Retry.MaxDelay)
			}
			if r.Exponent > c.Message.Retry.Exponent {
				return fmt.Errorf("MessageMin.Retry.Exponent (%d) must be <= Message.Retry.Exponent (%d)", r.Exponent, c.Message.Retry.Exponent)
			}
		}
	}
	if c.Message.Timeout > c.MessageMax.Timeout {
		return fmt.Errorf("Message.Timeout (%v) must be <= MessageMax.Timeout (%v)", c.Message.Timeout, c.MessageMax.Timeout)
	}
	if c.Message.Retry.MaxRetries > c.MessageMax.Retry.MaxRetries {
		return fmt.Errorf("Message.Retry.MaxRetries (%d) must be <= MessageMax.Retry.MaxRetries (%d)", c.Message.Retry.MaxRetries, c.MessageMax.Retry.MaxRetries)
	}
	if c.Message.Retry.BaseDelay > c.MessageMax.Retry.BaseDelay {
		return fmt.Errorf("Message.Retry.BaseDelay (%v) must be <= MessageMax.Retry.BaseDelay (%v)", c.Message.Retry.BaseDelay, c.MessageMax.Retry.BaseDelay)
	}
	if c.Message.Retry.MaxDelay > c.MessageMax.Retry.MaxDelay {
		return fmt.Errorf("Message.Retry.MaxDelay (%v) must be <= MessageMax.Retry.MaxDelay (%v)", c.Message.Retry.MaxDelay, c.MessageMax.Retry.MaxDelay)
	}
	if c.Message.Retry.Exponent > c.MessageMax.Retry.Exponent {
		return fmt.Errorf("Message.Retry.Exponent (%d) must be <= MessageMax.Retry.Exponent (%d)", c.Message.Retry.Exponent, c.MessageMax.Retry.Exponent)
	}
	return nil
}
