package admin

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// RunCronJobConfig is RunCronJob's spec -- every field is optional.
type RunCronJobConfig struct {
	// Concurrency - the produced request's concurrent-run policy.
	// Default: parallel (the request runs even while a previous one is still running).
	//
	// Set exclusive to run early WITHOUT overlapping a request already running.
	Concurrency common.ConcurrencyPolicy
}

func (c *RunCronJobConfig) WithDefaults() *RunCronJobConfig {
	if c.Concurrency == "" {
		c.Concurrency = common.ConcurrencyParallel
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *RunCronJobConfig) Validate() error {
	if err := c.Concurrency.Validate(); err != nil {
		return fmt.Errorf("Concurrency: %w", err)
	}
	return nil
}
