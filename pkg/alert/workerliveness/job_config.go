package workerliveness

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/schedule"
)

// JobConfig is NewJob's spec -- the schedule the worker_liveness alert is
// evaluated on. The alert takes no threshold: a worker row either has a live
// instance or it does not.
type JobConfig struct {
	// Schedule - how often the alert is evaluated, as schedule.ParseExpression
	// accepts it.
	// Default: @hourly.
	Expression string
}

func (c *JobConfig) WithDefaults() *JobConfig {
	if c.Expression == "" {
		c.Expression = "@hourly"
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *JobConfig) Validate() error {
	if _, err := schedule.ParseExpression(c.Expression); err != nil {
		return fmt.Errorf("Schedule: %w", err)
	}
	return nil
}
