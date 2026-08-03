package controller

import (
	"fmt"
)

// WorkerConfig is InsertWorker's spec -- every field is optional.
type WorkerConfig struct {
	// Metadata - the worker's tuning knobs, stored as JSONB; each worker kind
	// owns its shape.
	// Default: nil (stored as '{}').
	Metadata any

	// TargetInstances - how many live instances of the worker should run at
	// once across every process.
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
	if c.TargetInstances < 0 {
		return fmt.Errorf("TargetInstances must be >= 0, got %d", c.TargetInstances)
	}
	return nil
}
