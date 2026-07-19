package retry

import (
	"fmt"
	"time"
)

// Policy is the tunable retry knobs that users see
type Policy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Exponent   int
}

func NewDefaultRetryPolicy() *Policy {
	return &Policy{
		MaxRetries: 6,
		BaseDelay:  time.Second,
		MaxDelay:   5 * time.Minute,
		Exponent:   2,
	}
}

func (p *Policy) WithDefaults() *Policy {
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
func (p *Policy) Validate() error {
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
