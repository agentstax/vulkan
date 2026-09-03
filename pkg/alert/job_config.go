package alert

import "fmt"

// PartitionCountJobConfig is the partition_count check's declaration: the
// schedule it is evaluated on and the count it alerts at.
type PartitionCountJobConfig struct {
	// Expression - how often the alert is evaluated, a cron expression.
	// Default: @hourly.
	Expression string

	// Threshold - the partition count on one topic at or above which the
	// alert is published.
	// Default: 0, which measures against half the lock ceiling Postgres
	// reports.
	Threshold int64
}

func (c *PartitionCountJobConfig) WithDefaults() *PartitionCountJobConfig {
	if c.Expression == "" {
		c.Expression = "@hourly"
	}
	return c
}

// Validate runs after WithDefaults; the expression is parsed where the
// schedule is declared.
func (c *PartitionCountJobConfig) Validate() error {
	if c.Threshold < 0 {
		return fmt.Errorf("Threshold must be >= 0, got %d", c.Threshold)
	}
	return nil
}

// CompactionReadCostJobConfig is the compaction_read_cost check's
// declaration: the schedule it is evaluated on and the cost it alerts at.
type CompactionReadCostJobConfig struct {
	// Expression - how often the alert is evaluated, a cron expression.
	// Default: @hourly.
	Expression string

	// Threshold - the read cost at or above which the alert is published.
	// Default: 0, the check's own ceiling.
	Threshold int64
}

func (c *CompactionReadCostJobConfig) WithDefaults() *CompactionReadCostJobConfig {
	if c.Expression == "" {
		c.Expression = "@hourly"
	}
	return c
}

// Validate runs after WithDefaults; the expression is parsed where the
// schedule is declared.
func (c *CompactionReadCostJobConfig) Validate() error {
	if c.Threshold < 0 {
		return fmt.Errorf("Threshold must be >= 0, got %d", c.Threshold)
	}
	return nil
}

// WorkerLivenessJobConfig is the worker_liveness check's declaration: the
// schedule it is evaluated on.
type WorkerLivenessJobConfig struct {
	// Expression - how often the alert is evaluated, a cron expression.
	// Default: @hourly.
	Expression string
}

func (c *WorkerLivenessJobConfig) WithDefaults() *WorkerLivenessJobConfig {
	if c.Expression == "" {
		c.Expression = "@hourly"
	}
	return c
}

// Validate runs after WithDefaults; the expression is parsed where the
// schedule is declared.
func (c *WorkerLivenessJobConfig) Validate() error {
	return nil
}
