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
	// Default: "" (no routing key; every group receives it regardless of binding).
	//
	// "" is stored as no routing key, not an empty-string match.
	// Ex: "orders.created", "billing.invoice.paid"
	RoutingKey string

	// Compaction - opts this message into log compaction: it becomes one
	// version of a key whose claims only ever return the latest version, not
	// every version ever written. Build with NewCompactionOptions.
	// Default: nil (not part of a compacted stream; delivered independently,
	// never superseded).
	//
	// A hot key caps batched throughput: same-key batches commit one after another.
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
	if o.Message != nil && o.Message.Concurrency == common.ConcurrencyDefer && o.Compaction == nil {
		return errors.New("Concurrency 'defer' set without Compaction -- defer has nothing to defer on, set Compaction too")
	}
	if err := o.Compaction.Validate(); err != nil {
		return fmt.Errorf("Compaction: %w", err)
	}
	if err := o.Message.Validate(); err != nil {
		return fmt.Errorf("Message: %w", err)
	}
	return nil
}

// CompactionOptions is ProduceOptions.Compaction: the key a message compacts
// under, and the rank that decides the key's winner.
type CompactionOptions struct {
	// Key - the compaction key claims resolve to the latest version of.
	// Ex: "user:123", "session:abc-def"
	Key string

	// Rank - overrides arrival order when picking the Key's winner: higher
	// rank wins, equal ranks fall to the id tiebreak. 0 means arrival order
	// decides.
	//
	// Rank is a COMMITMENT, not a hint: a high-rank write pins its key --
	// lower ranks lose silently until something >= it arrives.
	// Ex: a source system's row version, a priority tier, epoch micros.
	Rank int64
}

// NewCompactionOptions builds the Compaction option for a produce. Pass rank
// 0 to let arrival order pick the key's winner.
func NewCompactionOptions(key string, rank int64) (*CompactionOptions, error) {
	if key == "" {
		return nil, errors.New("compaction key is required")
	}
	return &CompactionOptions{Key: key, Rank: rank}, nil
}

// Validate tolerates a nil receiver -- nil means not compacted.
func (o *CompactionOptions) Validate() error {
	if o == nil {
		return nil
	}
	if o.Key == "" {
		return fmt.Errorf("Key is required -- Rank %d has nothing to rank; build with NewCompactionOptions", o.Rank)
	}
	return nil
}
