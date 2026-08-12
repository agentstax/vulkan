package controller

import (
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
)

// CronJobConfig is RegisterCronJob's optional knobs.
type CronJobConfig struct {
	// Timeout - how long one job request's delivery may run.
	// Default: 30s.
	Timeout time.Duration

	// Concurrency - same-key policy when a job request lands while a previous
	// one still runs.
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

// AlterCronJobConfig is Alter's sparse patch -- an unset field means leave
// unchanged (for Data/Metadata that means nil, where Register's nil = {}).
// Name is absent -- it is the routing key consumers bind; a different name is
// a different job, not a config change.
type AlterCronJobConfig struct {
	Schedule    *cron.Schedule
	Timeout     *time.Duration
	Concurrency common.ConcurrencyPolicy // "" = unchanged
	Data        any
	Metadata    any
}

func (c *AlterCronJobConfig) Validate() error {
	if c.Schedule == nil && c.Timeout == nil && c.Concurrency == "" && c.Data == nil && c.Metadata == nil {
		return errors.New("no fields set -- an alter must change at least one field")
	}
	if c.Timeout != nil && *c.Timeout <= 0 {
		return fmt.Errorf("Timeout must be > 0, got %v", *c.Timeout)
	}
	if c.Concurrency != "" {
		if err := c.Concurrency.Validate(); err != nil {
			return fmt.Errorf("Concurrency: %w", err)
		}
	}
	return nil
}
