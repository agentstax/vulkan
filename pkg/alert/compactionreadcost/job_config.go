package compactionreadcost

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/cron"
)

// JobConfig is NewJob's spec -- the cron job the compaction_read_cost alert
// is evaluated on.
type JobConfig struct {
	// Schedule - how often the alert is evaluated, as cron.ParseSchedule
	// accepts it.
	// Default: @hourly.
	Schedule string

	// Threshold - the partition count one compacted message key's replay may span
	// before the alert is published.
	// Default: 0, which measures against 10,000 partitions.
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
