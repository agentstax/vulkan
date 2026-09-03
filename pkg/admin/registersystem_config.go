package admin

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/system"
)

// RegisterSystemConfig is RegisterSystem's spec -- the system's own settings
// plus the schedule each built-in alert is evaluated on. Every field is
// optional.
type RegisterSystemConfig struct {
	// System - the control-plane settings scoped to no single topic.
	// Default: SystemConfig's own defaults.
	System *system.SystemConfig

	// PartitionCount - the schedule the partition_count alert is evaluated on.
	// Default: its own defaults.
	PartitionCount *alert.PartitionCountJobConfig

	// CompactionReadCost - the schedule the compaction_read_cost alert is
	// evaluated on.
	// Default: its own defaults.
	CompactionReadCost *alert.CompactionReadCostJobConfig

	// WorkerLiveness - the schedule the worker_liveness alert is evaluated on.
	// Default: its own defaults.
	WorkerLiveness *alert.WorkerLivenessJobConfig
}

func (c *RegisterSystemConfig) WithDefaults() *RegisterSystemConfig {
	if c.System == nil {
		c.System = &system.SystemConfig{}
	}
	c.System.WithDefaults()
	if c.PartitionCount == nil {
		c.PartitionCount = &alert.PartitionCountJobConfig{}
	}
	c.PartitionCount.WithDefaults()
	if c.CompactionReadCost == nil {
		c.CompactionReadCost = &alert.CompactionReadCostJobConfig{}
	}
	c.CompactionReadCost.WithDefaults()
	if c.WorkerLiveness == nil {
		c.WorkerLiveness = &alert.WorkerLivenessJobConfig{}
	}
	c.WorkerLiveness.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *RegisterSystemConfig) Validate() error {
	if err := c.System.Validate(); err != nil {
		return fmt.Errorf("System: %w", err)
	}
	if err := c.PartitionCount.Validate(); err != nil {
		return fmt.Errorf("PartitionCount: %w", err)
	}
	if err := c.CompactionReadCost.Validate(); err != nil {
		return fmt.Errorf("CompactionReadCost: %w", err)
	}
	if err := c.WorkerLiveness.Validate(); err != nil {
		return fmt.Errorf("WorkerLiveness: %w", err)
	}
	return nil
}
