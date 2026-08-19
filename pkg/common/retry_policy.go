package common

import (
	"fmt"
	"math"
	"time"
)

// RetryPolicy is the tunable retry config
type RetryPolicy struct {
	MaxRetries int           `json:"max_retries,omitempty"`
	BaseDelay  time.Duration `json:"base_delay,omitempty"`
	MaxDelay   time.Duration `json:"max_delay,omitempty"`
	Exponent   int           `json:"exponent,omitempty"`
}

func NewDefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries: 6,
		BaseDelay:  time.Second,
		MaxDelay:   5 * time.Minute,
		Exponent:   2,
	}
}

// CalculateDelay returns the clamped exponential backoff
// Algo: BaseDelay * Exponent^attempt, floored at 0 and ceiled at MaxDelay.
func (p *RetryPolicy) CalculateDelay(attempt int) time.Duration {
	delay := time.Duration(float64(p.BaseDelay) * math.Pow(float64(p.Exponent), float64(attempt)))
	return max(MIN_DELAY, min(delay, p.MaxDelay))
}

// CalculateTotalDelay returns the schedule's total sleep time. Wrap never
// sleeps after the last attempt, so the sum stops at MaxRetries-2.
func (p *RetryPolicy) CalculateTotalDelay() time.Duration {
	var total time.Duration
	for attempt := range p.MaxRetries - 1 {
		total += p.CalculateDelay(attempt)
	}
	return total
}

func (p *RetryPolicy) Equal(other *RetryPolicy) bool {
	if p == nil || other == nil {
		return p == other
	}
	return *p == *other
}

func (p *RetryPolicy) WithDefaults() *RetryPolicy {
	if p == nil {
		return NewDefaultRetryPolicy()
	}

	// set defaults for any non-set values
	defaults := NewDefaultRetryPolicy()
	if p.MaxRetries == 0 {
		p.MaxRetries = defaults.MaxRetries
	}
	if p.BaseDelay == 0 {
		p.BaseDelay = defaults.BaseDelay
	}
	if p.MaxDelay == 0 {
		p.MaxDelay = defaults.MaxDelay
	}
	if p.Exponent == 0 {
		p.Exponent = defaults.Exponent
	}
	return p
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (p *RetryPolicy) Validate() error {
	if p == nil {
		return nil // nil is valid -- it resolves to the default policy at use
	}

	// MaxRetries < 1 makes Wrap's loop run ZERO times -- it would return nil
	// without ever calling the wrapped func, a silent fake success
	if p.MaxRetries < 1 {
		return fmt.Errorf("MaxRetries must be >= 1, got %d", p.MaxRetries)
	}

	// non-positive BaseDelay/MaxDelay clamp every backoff to 0 -- transient
	// errors would retry in a hot loop
	if p.BaseDelay <= 0 {
		return fmt.Errorf("BaseDelay must be > 0, got %v", p.BaseDelay)
	}
	if p.MaxDelay <= 0 {
		return fmt.Errorf("MaxDelay must be > 0, got %v", p.MaxDelay)
	}
	if p.MaxDelay < p.BaseDelay {
		return fmt.Errorf("MaxDelay (%v) must be >= BaseDelay (%v)", p.MaxDelay, p.BaseDelay)
	}

	// Exponent < 1 flips CalculateDelay's sign on alternating attempts
	if p.Exponent < 1 {
		return fmt.Errorf("Exponent must be >= 1, got %d", p.Exponent)
	}
	return nil
}
