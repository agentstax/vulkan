package retry

import "time"

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
