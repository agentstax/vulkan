package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

// CronJobConfig is RegisterCronJob's optional knobs.
type CronJobConfig struct {
	// Timeout - how long one job request's delivery may run.
	// Default: 30s.
	Timeout time.Duration

	// Concurrency - whether a job request runs while a previous one is still
	// running (allow) or waits for it (defer).
	// Default: allow.
	Concurrency common.ConcurrencyPolicy

	// Metadata - marshaled to opaque JSON carried on every job request.
	// Default: {}.
	Metadata any
}

func (c *CronJobConfig) WithDefaults() *CronJobConfig {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.Concurrency == "" {
		c.Concurrency = common.ConcurrencyAllow
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *CronJobConfig) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("Timeout must be > 0, got %v", c.Timeout)
	}
	if err := c.Concurrency.Validate(); err != nil {
		return fmt.Errorf("Concurrency: %w", err)
	}
	return nil
}
