package admin

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost"
	"github.com/agentstax/vulkan/pkg/alert/partitioncount"
	"github.com/agentstax/vulkan/pkg/alert/workerliveness"
	systemcontroller "github.com/agentstax/vulkan/pkg/system/controller"
)

// RegisterSystemConfig is RegisterSystem's spec -- the system's own settings
// plus the schedule each built-in alert is evaluated on. Every field is
// optional.
type RegisterSystemConfig struct {
	// System - the control-plane settings scoped to no single topic.
	// Default: SystemConfig's own defaults.
	System *systemcontroller.SystemConfig

	// PartitionCount - the schedule the partition_count alert is evaluated on.
	// Default: JobConfig's own defaults.
	PartitionCount *partitioncount.JobConfig

	// CompactionReadCost - the schedule the compaction_read_cost alert is
	// evaluated on.
	// Default: JobConfig's own defaults.
	CompactionReadCost *compactionreadcost.JobConfig

	// WorkerLiveness - the schedule the worker_liveness alert is evaluated on.
	// Default: JobConfig's own defaults.
	WorkerLiveness *workerliveness.JobConfig
}

func (c *RegisterSystemConfig) WithDefaults() *RegisterSystemConfig {
	if c.System == nil {
		c.System = &systemcontroller.SystemConfig{}
	}
	c.System.WithDefaults()
	if c.PartitionCount == nil {
		c.PartitionCount = &partitioncount.JobConfig{}
	}
	c.PartitionCount.WithDefaults()
	if c.CompactionReadCost == nil {
		c.CompactionReadCost = &compactionreadcost.JobConfig{}
	}
	c.CompactionReadCost.WithDefaults()
	if c.WorkerLiveness == nil {
		c.WorkerLiveness = &workerliveness.JobConfig{}
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
