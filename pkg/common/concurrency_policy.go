package common

import "fmt"

// ConcurrencyPolicy is a message's concurrency policy.
type ConcurrencyPolicy string

const (
	ConcurrencyAllow ConcurrencyPolicy = "allow" // current key busy -> new same-keyed message runs concurrently
	ConcurrencyDefer ConcurrencyPolicy = "defer" // current key busy -> new same-keyed message waits; when the key frees, only the key's most recent head runs
)

func (p ConcurrencyPolicy) Validate() error {
	switch p {
	case "", ConcurrencyAllow, ConcurrencyDefer:
		return nil
	default:
		return fmt.Errorf("must be one of %q, %q, got %q", ConcurrencyAllow, ConcurrencyDefer, p)
	}
}
