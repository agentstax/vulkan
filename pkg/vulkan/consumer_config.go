package vulkan

import (
	"time"

	"github.com/agentstax/vulkan/pkg/consumergroup"
)

// ConsumerConfig is the group's declaration: what the group means, identical
// for every instance of the group. How one session runs is ConsumeOptions at
// Consume. Ambient (logger, retry) lives on ClientConfig; defaults fill and
// validation runs when the client builds its consumer.
type ConsumerConfig struct {
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

	// Bindings - the group's whole pattern set. Default: nil (the whole topic).
	Bindings []string

	ExceptionInitialBackoff time.Duration // can_run_after delay when an exception/terminal row is first written (Commit/PartialCommit) -- Message.Retry takes over on later retries
	MaxRangeReclaims        int           // past this many reclaims a range is POISON -- quarantined into the exception window instead of handed out again
}
