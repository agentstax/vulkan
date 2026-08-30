package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// ScheduleConfig is RegisterSchedule's spec -- every field is optional.
type ScheduleConfig struct {
	// Timeout - how long one job request's delivery may run.
	// Default: 30s.
	Timeout time.Duration

	// Concurrency - whether a job request runs while a previous one is still
	// running (parallel) or waits for it (exclusive).
	// Default: parallel.
	Concurrency common.ConcurrencyPolicy

	// Metadata - marshaled to opaque JSON carried on every job request.
	// Default: {}.
	Metadata any
}

func (c *ScheduleConfig) WithDefaults() *ScheduleConfig {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.Concurrency == "" {
		c.Concurrency = common.ConcurrencyParallel
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ScheduleConfig) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("Timeout must be > 0, got %v", c.Timeout)
	}
	if err := c.Concurrency.Validate(); err != nil {
		return fmt.Errorf("Concurrency: %w", err)
	}
	return nil
}
