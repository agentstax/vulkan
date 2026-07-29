package common

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/retry"
)

// ConcurrencyPolicy is a message's concurrency policy.
type ConcurrencyPolicy string

const (
	ConcurrencyAllow  ConcurrencyPolicy = "allow"  // current key busy -> new same-keyed message run concurrently
	ConcurrencyForbid ConcurrencyPolicy = "forbid" // current key busy -> new same-keyed message is dropped
	ConcurrencyDefer  ConcurrencyPolicy = "defer"  // current key busy -> new same-keyed message is queued to run after current finishes
)

func (p ConcurrencyPolicy) Validate() error {
	switch p {
	case "", ConcurrencyAllow, ConcurrencyForbid, ConcurrencyDefer:
		return nil
	default:
		return fmt.Errorf("must be one of %q, %q, %q, got %q", ConcurrencyAllow, ConcurrencyForbid, ConcurrencyDefer, p)
	}
}

// MessageOptions are the per-message knobs a producer may REQUEST and a
// consumer may CLAMP. Any unset field means "the consumer decides".
//
// Resolution is per field:
// - consumer clamp > produced message > consumer defaults > system defaults
// Messages REQUEST, consumers PROTECT THEMSELVES.
type MessageOptions struct {
	// Concurrency - Concurrency policy for this message's compaction key.
	// Requires a CompactionKey.
	// Default: ConcurrencyAllow (allow overlapping concurrent per-key processing)
	Concurrency ConcurrencyPolicy `json:"concurrency,omitempty"`

	// WorkTimeout - how long this message's consumerFunc may run.
	// Default: 0 (the consumer's own WorkTimeout applies).
	WorkTimeout time.Duration `json:"work_timeout,omitempty"`

	// Retry - redelivery policy for this message. Unset fields fall
	// to the consumer's policy per-field.
	// Default: nil (the consumer's policy applies whole).
	Retry *retry.Policy `json:"retry,omitempty"`
}

func (o *MessageOptions) WithDefaults() *MessageOptions {
	if o == nil {
		o = &MessageOptions{}
	}
	if o.WorkTimeout == 0 {
		o.WorkTimeout = 30 * time.Second
	}
	if o.Retry == nil {
		o.Retry = &retry.Policy{}
	}
	if o.Retry.MaxRetries == 0 {
		o.Retry.MaxRetries = 3 // redelivery caps at 3 attempts by default -- the Policy default of 6 is tuned for internal retries
	}
	o.Retry = o.Retry.WithDefaults()
	return o
}

func (o *MessageOptions) Fill(defaults *MessageOptions) *MessageOptions {
	if o == nil {
		return defaults
	}
	if defaults == nil {
		return o
	}
	filled := *o
	if filled.Concurrency == "" {
		filled.Concurrency = defaults.Concurrency
	}
	if filled.WorkTimeout == 0 {
		filled.WorkTimeout = defaults.WorkTimeout
	}
	filled.Retry = fillPolicy(filled.Retry, defaults.Retry)
	return &filled
}

func (o *MessageOptions) Clamp(min, max *MessageOptions) *MessageOptions {
	if o == nil || (min == nil && max == nil) {
		return o
	}
	var lo, hi MessageOptions
	if min != nil {
		lo = *min
	}
	if max != nil {
		hi = *max
	}
	clamped := *o
	clamped.WorkTimeout = clampDuration(clamped.WorkTimeout, lo.WorkTimeout, hi.WorkTimeout)
	clamped.Retry = clampPolicy(clamped.Retry, lo.Retry, hi.Retry)
	return &clamped
}

func (o *MessageOptions) Validate() error {
	if o == nil {
		return nil
	}
	if err := o.Concurrency.Validate(); err != nil {
		return fmt.Errorf("Concurrency: %w", err)
	}
	if o.WorkTimeout < 0 {
		return fmt.Errorf("WorkTimeout must be >= 0, got %v", o.WorkTimeout)
	}
	if o.Retry != nil {
		if err := validateSparsePolicy(o.Retry); err != nil {
			return fmt.Errorf("Retry: %w", err)
		}
	}
	return nil
}

func validateSparsePolicy(p *retry.Policy) error {
	if p.MaxRetries < 0 {
		return fmt.Errorf("MaxRetries must be >= 0, got %d", p.MaxRetries)
	}
	if p.BaseDelay < 0 {
		return fmt.Errorf("BaseDelay must be >= 0, got %v", p.BaseDelay)
	}
	if p.MaxDelay < 0 {
		return fmt.Errorf("MaxDelay must be >= 0, got %v", p.MaxDelay)
	}
	if p.BaseDelay > 0 && p.MaxDelay > 0 && p.MaxDelay < p.BaseDelay {
		return fmt.Errorf("MaxDelay (%v) must be >= BaseDelay (%v)", p.MaxDelay, p.BaseDelay)
	}
	if p.Exponent < 0 {
		return fmt.Errorf("Exponent must be >= 0, got %d", p.Exponent)
	}
	return nil
}

func fillPolicy(p, defaults *retry.Policy) *retry.Policy {
	if p == nil {
		return defaults
	}
	if defaults == nil {
		return p
	}
	merged := *p
	if merged.MaxRetries == 0 {
		merged.MaxRetries = defaults.MaxRetries
	}
	if merged.BaseDelay == 0 {
		merged.BaseDelay = defaults.BaseDelay
	}
	if merged.MaxDelay == 0 {
		merged.MaxDelay = defaults.MaxDelay
	}
	if merged.Exponent == 0 {
		merged.Exponent = defaults.Exponent
	}
	return &merged
}

func clampPolicy(p, min, max *retry.Policy) *retry.Policy {
	if p == nil || (min == nil && max == nil) {
		return p
	}
	var lo, hi retry.Policy
	if min != nil {
		lo = *min
	}
	if max != nil {
		hi = *max
	}
	clamped := *p
	clamped.MaxRetries = clampInt(clamped.MaxRetries, lo.MaxRetries, hi.MaxRetries)
	clamped.BaseDelay = clampDuration(clamped.BaseDelay, lo.BaseDelay, hi.BaseDelay)
	clamped.MaxDelay = clampDuration(clamped.MaxDelay, lo.MaxDelay, hi.MaxDelay)
	clamped.Exponent = clampInt(clamped.Exponent, lo.Exponent, hi.Exponent)
	return &clamped
}

func clampDuration(v, min, max time.Duration) time.Duration {
	if min > 0 && v < min {
		v = min
	}
	if max > 0 && v > max {
		v = max
	}
	return v
}

func clampInt(v, min, max int) int {
	if min > 0 && v < min {
		v = min
	}
	if max > 0 && v > max {
		v = max
	}
	return v
}
