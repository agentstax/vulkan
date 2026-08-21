package base

import (
	"fmt"
	"time"
)

// BaseConsumerConfig holds the knobs every row's shared machinery paces
// from -- the runner's own config keeps the rest.
type BaseConsumerConfig struct {
	TimeoutGrace          time.Duration // scheduling slack for a consumerFunc that DID respect ctx.Done() to unwind before the hard cutoff abandons it. Default: 100ms.
	RecordMargin          time.Duration // lease padding for recording success/failure after consumerFunc returns. Default: 2s.
	SlowDispatchThreshold time.Duration // a delivery dispatch running longer than this logs a warn line with its duration. Default: 0 (disabled).
}

func (c *BaseConsumerConfig) WithDefaults() *BaseConsumerConfig {
	if c.TimeoutGrace == 0 {
		c.TimeoutGrace = 100 * time.Millisecond
	}
	if c.RecordMargin == 0 {
		c.RecordMargin = 2 * time.Second
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *BaseConsumerConfig) Validate() error {
	if c.TimeoutGrace <= 0 {
		return fmt.Errorf("TimeoutGrace must be > 0, got %v", c.TimeoutGrace)
	}
	if c.RecordMargin <= 0 {
		return fmt.Errorf("RecordMargin must be > 0, got %v", c.RecordMargin)
	}
	if c.SlowDispatchThreshold < 0 {
		return fmt.Errorf("SlowDispatchThreshold must be >= 0, got %v", c.SlowDispatchThreshold)
	}
	return nil
}
