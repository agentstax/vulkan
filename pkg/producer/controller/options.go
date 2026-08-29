package controller

import (
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/google/uuid"
)

// ProduceOptions holds per-message knobs that are optional and rarely set --
// the zero value means "neither is set," so a caller who doesn't need them
// never has to name them.
type ProduceOptions struct {
	// RoutingKey - matched against a consumer group's bindings to decide
	// whether that group receives this message at all.
	// Default: "" (no routing key; a keyless message matches no binding, so
	// only groups with no bindings receive it).
	//
	// "" is stored as no routing key, not an empty-string match.
	// Ex: "orders.created", "billing.invoice.paid"
	RoutingKey string

	// MessageKey - the entity this message is about. On its own it is stored
	// and nothing more; Compaction and ConcurrencyExclusive both read it.
	// Default: "" (no key).
	//
	// Ex: "user:123", "acct-42", a device serial.
	MessageKey string

	// Compaction - opts this message into log compaction under its
	// MessageKey: it becomes one version of the key, and claims only ever
	// return the key's latest version, not every version ever written.
	// Build with NewCompactionOptions.
	// Default: nil (not compacted; delivered independently, never superseded).
	//
	// A hot key caps batched throughput: same-key batches commit one after
	// another, and adding producer processes makes a hot key slower, not faster.
	Compaction *CompactionOptions

	// IdempotencyKey - protects a retried AppendMessage (after a blip) from double-publishing.
	// Default: uuid.Nil (a fresh key is generated per call, protecting only
	// against retries within that one call).
	//
	// Supply your own for protection across your OWN retries too -- e.g. your
	// process crashes and restarts before learning whether a publish landed,
	// and you call Produce again with the same key. Try to use a time-ordered key
	// (UUIDv7): random (v4) keys slow throughput down considerably.
	// A caller-supplied key routes the call to a per-call transaction, never a batch.
	// Ex: a UUIDv7 persisted alongside the work before the first Produce attempt.
	IdempotencyKey uuid.UUID

	// Message - per-message MessageOptions: what this message REQUESTS from
	// whoever consumes it (work timeout, redelivery policy, concurrency).
	// Default: nil (defaults to Producer Defaults > Consumer Defaults).
	Message *common.MessageOptions
}

// Validate rejects nonsensical option combinations.
// Must be called after Fill().
func (o ProduceOptions) Validate() error {
	if o.Message != nil && o.Message.Concurrency.HoldsKey() && o.MessageKey == "" {
		return fmt.Errorf("Concurrency %q set without MessageKey -- it runs deliveries one at a time per key, set ProduceOptions.MessageKey", o.Message.Concurrency)
	}
	if o.Message != nil && o.Message.Concurrency == common.ConcurrencyOrdered && o.Compaction != nil && o.Compaction.Enable {
		return errors.New("Concurrency 'ordered' set with Compaction enabled -- compaction supersedes older versions, ordered delivers every one; choose one")
	}
	if o.Compaction != nil && o.Compaction.Enable && o.MessageKey == "" {
		return errors.New("Compaction enabled without MessageKey -- compaction picks a winner per key, set ProduceOptions.MessageKey")
	}
	if err := o.Compaction.Validate(); err != nil {
		return fmt.Errorf("Compaction: %w", err)
	}
	if err := o.Message.Validate(); err != nil {
		return fmt.Errorf("Message: %w", err)
	}
	return nil
}

// CompactionOptions is ProduceOptions.Compaction: whether the message
// compacts under its MessageKey, and the rank that decides the key's winner.
type CompactionOptions struct {
	// Enable - opts the message into compaction. A nil Compaction and Enable
	// false mean the same thing: not compacted.
	Enable bool

	// Rank - overrides arrival order when picking the key's winner: higher
	// rank wins, equal ranks fall to the id tiebreak. 0 means arrival order
	// decides.
	//
	// Rank is a COMMITMENT, not a hint: a high-rank write pins its key --
	// lower ranks lose silently until something >= it arrives.
	// Ex: a source system's row version, a priority tier, epoch micros.
	Rank int64
}

// NewCompactionOptions builds the Compaction option for a produce, enabled.
// Pass rank 0 to let arrival order pick the key's winner.
func NewCompactionOptions(rank int64) (*CompactionOptions, error) {
	return &CompactionOptions{Enable: true, Rank: rank}, nil
}

// Validate tolerates a nil receiver -- nil means not compacted.
func (o *CompactionOptions) Validate() error {
	if o == nil {
		return nil
	}
	if !o.Enable && o.Rank != 0 {
		return fmt.Errorf("Rank must be 0 when Enable is false, got %d -- build with NewCompactionOptions", o.Rank)
	}
	return nil
}
