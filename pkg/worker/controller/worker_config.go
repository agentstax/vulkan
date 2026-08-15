package controller

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
)

// WorkerConfig is InsertWorker's spec -- every field is optional.
type WorkerConfig struct {
	// Metadata - the worker's tuning knobs, stored as JSONB; each worker kind
	// owns its shape.
	// Default: nil (stored as '{}').
	Metadata any

	// TargetInstances - how many live instances of the worker should run at
	// once across every process. worker.NoInstanceTarget lifts the gate.
	// Default: 1.
	TargetInstances int
}

func (c *WorkerConfig) WithDefaults() *WorkerConfig {
	if c.TargetInstances == 0 {
		c.TargetInstances = 1
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *WorkerConfig) Validate() error {
	if c.TargetInstances < worker.NoInstanceTarget {
		return fmt.Errorf("TargetInstances must be >= %d, got %d", worker.NoInstanceTarget, c.TargetInstances)
	}
	return nil
}

// AlterWorkerConfig is AlterWorker's spec.
type AlterWorkerConfig struct {
	// Overrides - metadata key -> the change to its override. A changed key
	// no targeted row declares fails the alter.
	Overrides map[string]common.Update[any]
}

func (c *AlterWorkerConfig) Validate() error {
	changed := false
	for key, update := range c.Overrides {
		if key == "" {
			return fmt.Errorf("Overrides keys must not be empty")
		}
		if update.IsChanged() {
			changed = true
		}
	}
	if !changed {
		return fmt.Errorf("no overrides set -- an alter must change at least one metadata key")
	}
	return nil
}
