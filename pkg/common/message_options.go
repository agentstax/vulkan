package common

import (
	"fmt"
	"time"
)

// MessageOptions are the per-message knobs a producer may REQUEST and a
// consumer may CLAMP. Any unset field means "the consumer decides".
//
// Resolution is per field:
// - consumer clamp > produced message > consumer defaults > system defaults
// Messages REQUEST, consumers PROTECT THEMSELVES.
type MessageOptions struct {
	// Concurrency - concurrency policy for this message's compaction key.
	// Requires a compaction key -- Defer without one errors at produce time.
	// Default: ConcurrencyAllow (same-key deliveries may overlap).
	Concurrency ConcurrencyPolicy `json:"concurrency,omitempty"`

	// Timeout - how long this message's consumerFunc may run.
	// Default: 0 (the consumer's own Timeout applies).
	Timeout time.Duration `json:"timeout,omitempty"`

	// Retry - redelivery policy for this message. Unset fields fall
	// to the consumer's policy per-field.
	// Default: nil (the consumer's policy applies whole).
	Retry *RetryPolicy `json:"retry,omitempty"`
}

// returns new copy not modified pointer
func (o *MessageOptions) Fill(defaults *MessageOptions) *MessageOptions {
	if o == nil && defaults == nil {
		return nil
	}

	var filled, d MessageOptions
	if o != nil {
		filled = *o
	}
	if defaults != nil {
		d = *defaults
	}
	if filled.Concurrency == "" {
		filled.Concurrency = d.Concurrency
	}
	if filled.Timeout == 0 {
		filled.Timeout = d.Timeout
	}
	filled.Retry = fillPolicy(filled.Retry, d.Retry)
	return &filled
}

// returns new copy not modified pointer
func (o *MessageOptions) Clamp(min, max *MessageOptions) *MessageOptions {
	if o == nil {
		return nil
	}

	var lo, hi MessageOptions
	if min != nil {
		lo = *min
	}
	if max != nil {
		hi = *max
	}
	clamped := *o
	clamped.Timeout = clampDuration(clamped.Timeout, lo.Timeout, hi.Timeout)
	clamped.Retry = clampPolicy(clamped.Retry, lo.Retry, hi.Retry)
	return &clamped
}

// returns new copy not modified pointer
func (o *MessageOptions) ResolveConcurrency(override ConcurrencyPolicy) *MessageOptions {
	var resolved MessageOptions
	if o != nil {
		resolved = *o
	}

	switch {
	case override != "":
		resolved.Concurrency = override
	case resolved.Concurrency == "":
		resolved.Concurrency = ConcurrencyAllow
	}
	return &resolved
}

func (o *MessageOptions) Equal(other *MessageOptions) bool {
	if o == nil || other == nil {
		return o == other
	}
	return o.Concurrency == other.Concurrency &&
		o.Timeout == other.Timeout &&
		o.Retry.Equal(other.Retry)
}

func (o *MessageOptions) WithDefaults() *MessageOptions {
	if o == nil {
		o = &MessageOptions{}
	}
	if o.Timeout == 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Retry == nil {
		o.Retry = &RetryPolicy{}
	}
	if o.Retry.MaxRetries == 0 {
		o.Retry.MaxRetries = 3 // redelivery caps at 3 attempts by default -- the Policy default of 6 is tuned for internal retries
	}
	o.Retry = o.Retry.WithDefaults()
	return o
}

func (o *MessageOptions) Validate() error {
	if o == nil {
		return nil
	}
	if err := o.Concurrency.Validate(); err != nil {
		return fmt.Errorf("Concurrency: %w", err)
	}
	if o.Timeout < 0 {
		return fmt.Errorf("Timeout must be >= 0, got %v", o.Timeout)
	}
	if o.Retry != nil {
		if err := validateSparsePolicy(o.Retry); err != nil {
			return fmt.Errorf("Retry: %w", err)
		}
	}
	return nil
}

// ***************
// *** HELPERS ***
// ***************

func validateSparsePolicy(p *RetryPolicy) error {
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

// returns new copy not modified pointer
func fillPolicy(p, defaults *RetryPolicy) *RetryPolicy {
	if p == nil && defaults == nil {
		return nil
	}

	var merged, d RetryPolicy
	if p != nil {
		merged = *p
	}
	if defaults != nil {
		d = *defaults
	}
	if merged.MaxRetries == 0 {
		merged.MaxRetries = d.MaxRetries
	}
	if merged.BaseDelay == 0 {
		merged.BaseDelay = d.BaseDelay
	}
	if merged.MaxDelay == 0 {
		merged.MaxDelay = d.MaxDelay
	}
	if merged.Exponent == 0 {
		merged.Exponent = d.Exponent
	}
	return &merged
}

// returns new copy not modified pointer
func clampPolicy(p, min, max *RetryPolicy) *RetryPolicy {
	if p == nil {
		return nil
	}

	var lo, hi RetryPolicy
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
