package vulkan

import (
	"time"

	"github.com/agentstax/vulkan/pkg/consumergroup"
)

// ConsumerConfig is the client's consumer declaration. Ambient (logger,
// retry) lives on ClientConfig; defaults fill and validation runs when the
// client builds its consumer.
type ConsumerConfig struct {
	BatchLimit         int
	QueueSize          int // claimed messages buffered ahead of processing -- must be >= BatchLimit or the prefetcher can never claim a full batch. Default: BatchLimit.
	MessageConcurrency int // messages processed concurrently. Default: 1.
	MaxRangeReclaims   int // past this many reclaims a range is POISON -- quarantined into the exception window instead of handed out again

	ClaimPollRate time.Duration
	QueueMargin   time.Duration // lease padding for time a claimed item sits queued before a worker starts on it
	RecordMargin  time.Duration // lease padding for recording success/failure after consumerFunc returns
	// TimeoutGrace is scheduling slack for a consumerFunc that DID respect
	// ctx.Done() to actually unwind and send on the result channel before the
	// hard cutoff abandons it -- not extra time to keep working. Default
	// assumes one same-region network round trip's worth of slack.
	TimeoutGrace time.Duration
	// SlowDispatchThreshold - a delivery dispatch running longer than this
	// logs a warn line with its duration; consumerFunc time is the dominant
	// term. Default: 0 (disabled).
	SlowDispatchThreshold   time.Duration
	ExceptionInitialBackoff time.Duration // can_run_after delay when an exception/terminal row is first written (Commit/PartialCommit) -- Message.Retry takes over on later retries

	InstanceTTL          time.Duration // how long this consumer's claimed worker_instance rows stay live without a heartbeat renewal -- past it a replacement can claim. Default: 30s.
	BindingRetryInterval time.Duration // how often Consume re-attempts a waiting binding declaration while a live instance still declares a different set. Default: 10s.

	ShutdownTimeout time.Duration // bounds how long drain waits for in-flight processClaim calls to finish before closeOpenRanges settles whatever's left. Default: MessageMax.Timeout + TimeoutGrace + RecordMargin.
	// DisableGracefulShutdown - lets Consume accept a context that can never
	// be cancelled (e.g. context.Background()), leaving process exit as the
	// only stop. Prefer passing the application's shutdown context to Consume.
	// Default: false.
	DisableGracefulShutdown bool

	// Message - default MessageOptions: fills any option the produced message left unset.
	// Default: Timeout 30s; Retry MaxRetries 3 with the default curve.
	Message *MessageOptions

	// MessageMin - per-option floors: raises any resolved option below these.
	// Concurrency is not orderable and must stay unset.
	// Default: nil (no floors).
	MessageMin *MessageOptions

	// MessageMax - per-option ceilings: lowers any resolved option above these.
	// Concurrency is not orderable and must stay unset.
	// Default: Message's values -- messages cannot request above the group's
	// defaults unless raised here.
	MessageMax *MessageOptions

	// ConcurrencyOverride - this group runs every message under this policy,
	// beating whatever the message requested.
	// Default: "" (honor each message's own policy).
	ConcurrencyOverride ConcurrencyPolicy

	// Start - where a group's cursor is placed when Register creates it;
	// a group that already has a cursor row keeps its position.
	// Default: consumergroup.Beginning() -- the oldest retained message.
	Start consumergroup.CursorPosition
}
