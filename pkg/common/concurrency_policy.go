package common

import "fmt"

// ConcurrencyPolicy is a message's concurrency policy.
type ConcurrencyPolicy string

const (
	ConcurrencyParallel  ConcurrencyPolicy = "parallel"  // same-key deliveries may overlap
	ConcurrencyExclusive ConcurrencyPolicy = "exclusive" // one delivery per key at a time: a same-key message finding the key busy is deferred and runs when the key frees, oldest first (with compaction: the key's current head)
)

func (p ConcurrencyPolicy) Validate() error {
	switch p {
	case "", ConcurrencyParallel, ConcurrencyExclusive:
		return nil
	default:
		return fmt.Errorf("must be one of %q, %q, got %q", ConcurrencyParallel, ConcurrencyExclusive, p)
	}
}
