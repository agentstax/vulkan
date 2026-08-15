package partitioncount

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/cron"
)

// JobConfig is NewJob's spec -- the cron job the partition_count alert is
// evaluated on.
type JobConfig struct {
	// Schedule - how often the alert is evaluated, as cron.ParseSchedule
	// accepts it.
	// Default: @hourly.
	Schedule string

	// Threshold - the partition count on one topic at or above which the
	// alert is published.
	// Default: 0, which measures against half the lock ceiling Postgres
	// reports.
	Threshold int64
}

func (c *JobConfig) WithDefaults() *JobConfig {
	if c.Schedule == "" {
		c.Schedule = "@hourly"
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *JobConfig) Validate() error {
	if _, err := cron.ParseSchedule(c.Schedule); err != nil {
		return fmt.Errorf("Schedule: %w", err)
	}
	if c.Threshold < 0 {
		return fmt.Errorf("Threshold must be >= 0, got %d", c.Threshold)
	}
	return nil
}
