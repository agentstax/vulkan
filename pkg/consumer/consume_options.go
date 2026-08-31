package consumer

import (
	"fmt"
	"time"
)

// ConsumeOptions is one Consume call's session settings -- how this process
// runs, free to differ per instance. What the group means lives on
// ConsumerConfig at Register. Sparse: zero fields take the defaults.
type ConsumeOptions struct {
	BatchLimit         int
	QueueSize          int // claimed messages buffered ahead of processing -- must be >= BatchLimit or the prefetcher can never claim a full batch. Default: BatchLimit.
	MessageConcurrency int // messages processed concurrently. Default: 1.

	ClaimPollRate time.Duration
	QueueMargin   time.Duration // lease padding for time a claimed item sits queued before a worker starts on it
	RecordMargin  time.Duration // lease padding for recording success/failure after consumerFunc returns
	// TimeoutGrace is scheduling slack for a consumerFunc that DID respect
	// ctx.Done() to actually unwind and send on the result channel before the
	// hard cutoff abandons it -- not extra time to keep working. Go's own
	// scheduler wakeup after a context deadline fires is sub-millisecond at p99
	// even under load (measured); this budget is really covering the caller's
	// own cancellation-response time (e.g. a DB driver's cancel-request round
	// trip), which pkg/consumer can't know in general. Default assumes one
	// same-region network round trip's worth of slack.
	TimeoutGrace time.Duration
	// SlowDispatchThreshold - a delivery dispatch running longer than this
	// logs a warn line with its duration; consumerFunc time is the dominant
	// term. Default: 0 (disabled).
	SlowDispatchThreshold time.Duration

	InstanceTTL          time.Duration // how long this consumer's claimed worker_instance rows stay live without a heartbeat renewal -- past it a replacement can claim. Default: 30s.
	BindingRetryInterval time.Duration // how often Consume re-attempts a waiting binding declaration while a live instance still declares a different set. Default: 10s.

	ShutdownTimeout time.Duration // bounds how long drain waits for in-flight processClaim calls to finish before closeOpenRanges settles whatever's left. Default: MessageMax.Timeout + TimeoutGrace + RecordMargin -- one callSafely's worst case at the ceiling a message may request, plus recording its outcome
	// DisableGracefulShutdown - lets Consume accept a context that can never
	// be cancelled (e.g. context.Background()), leaving process exit as the
	// only stop. Prefer passing the application's shutdown context to Consume.
	// Default: false.
	DisableGracefulShutdown bool
}

// WithDefaults leaves ShutdownTimeout at zero: its default needs the group's
// MessageMax, so Consume derives it after this fills.
func (o *ConsumeOptions) WithDefaults() *ConsumeOptions {
	if o.BatchLimit == 0 {
		o.BatchLimit = 1 // no batching by default
	}

	if o.QueueSize == 0 {
		o.QueueSize = o.BatchLimit
	}

	if o.MessageConcurrency == 0 {
		o.MessageConcurrency = 1
	}

	if o.ClaimPollRate == 0 {
		o.ClaimPollRate = 5 * time.Second
	}

	if o.QueueMargin == 0 {
		o.QueueMargin = 5 * time.Second
	}

	if o.RecordMargin == 0 {
		o.RecordMargin = 2 * time.Second
	}

	if o.TimeoutGrace == 0 {
		o.TimeoutGrace = 100 * time.Millisecond
	}

	if o.InstanceTTL == 0 {
		o.InstanceTTL = 30 * time.Second
	}

	if o.BindingRetryInterval == 0 {
		o.BindingRetryInterval = 10 * time.Second
	}

	return o
}

// Validate runs after WithDefaults and the ShutdownTimeout derivation --
// anything still out of range here was set by the caller, not left unset.
func (o *ConsumeOptions) Validate() error {
	if o.BatchLimit < 1 {
		return fmt.Errorf("BatchLimit must be >= 1, got %d", o.BatchLimit)
	}
	if o.QueueSize < o.BatchLimit {
		return fmt.Errorf("QueueSize (%d) must be >= BatchLimit (%d), otherwise the prefetcher can never claim a full batch", o.QueueSize, o.BatchLimit)
	}
	if o.MessageConcurrency < 1 {
		return fmt.Errorf("MessageConcurrency must be >= 1, got %d", o.MessageConcurrency)
	}

	// non-positive durations break their respective loops/timers:
	// ClaimPollRate<=0 -> the prefetch back-off and WaitForRoom timers fire immediately (busy loop),
	// QueueMargin/RecordMargin<=0 -> the lease window math degenerates.
	if o.ClaimPollRate <= 0 {
		return fmt.Errorf("ClaimPollRate must be > 0, got %v", o.ClaimPollRate)
	}
	if o.QueueMargin <= 0 {
		return fmt.Errorf("QueueMargin must be > 0, got %v", o.QueueMargin)
	}
	if o.RecordMargin <= 0 {
		return fmt.Errorf("RecordMargin must be > 0, got %v", o.RecordMargin)
	}
	if o.TimeoutGrace <= 0 {
		return fmt.Errorf("TimeoutGrace must be > 0, got %v", o.TimeoutGrace)
	}
	if o.SlowDispatchThreshold < 0 {
		return fmt.Errorf("SlowDispatchThreshold must be >= 0, got %v", o.SlowDispatchThreshold)
	}
	if o.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", o.InstanceTTL)
	}
	if o.BindingRetryInterval <= 0 {
		return fmt.Errorf("BindingRetryInterval must be > 0, got %v", o.BindingRetryInterval)
	}
	if o.ShutdownTimeout <= 0 {
		return fmt.Errorf("ShutdownTimeout must be > 0, got %v", o.ShutdownTimeout)
	}
	return nil
}
