package controller

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/worker"
)

// WorkerConfig is RegisterWorker's spec -- every field is optional.
type WorkerConfig struct {
	// Metadata - the worker's own config, stored as JSONB; each worker kind
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
