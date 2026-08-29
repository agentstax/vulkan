package common

import "fmt"

// ConcurrencyPolicy is a message's concurrency policy.
type ConcurrencyPolicy string

const (
	ConcurrencyParallel  ConcurrencyPolicy = "parallel"  // same-key deliveries may overlap
	ConcurrencyExclusive ConcurrencyPolicy = "exclusive" // one delivery per key at a time: a same-key message finding the key busy is deferred and runs when the key frees, oldest first (with compaction: the key's current head)
	ConcurrencyOrdered   ConcurrencyPolicy = "ordered"   // exclusive, and a keyed message runs only after every earlier same-key message is resolved for the group -- a failed predecessor's retry goes first, dead does not hold the key
)

func (p ConcurrencyPolicy) Validate() error {
	switch p {
	case "", ConcurrencyParallel, ConcurrencyExclusive, ConcurrencyOrdered:
		return nil
	default:
		return fmt.Errorf("must be one of %q, %q, %q, got %q", ConcurrencyParallel, ConcurrencyExclusive, ConcurrencyOrdered, p)
	}
}

// HoldsKey reports whether deliveries under the policy claim the message
// key lease before running.
func (p ConcurrencyPolicy) HoldsKey() bool {
	return p == ConcurrencyExclusive || p == ConcurrencyOrdered
}
