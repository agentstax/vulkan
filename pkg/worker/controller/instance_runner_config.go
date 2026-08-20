package controller

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

type InstanceRunnerConfig struct {
	// InstanceTTL is how long the claimed worker_instance row stays live
	// without a renewal -- past it the instance counts as dead and a
	// replacement can claim. The heartbeat renews at half this.
	// Default: 30s.
	InstanceTTL time.Duration

	Logger common.Logger // enrich with the worker's identity via common.LoggerWith. Default: text lines to stderr, warn level and up.
}

func (c *InstanceRunnerConfig) WithDefaults() *InstanceRunnerConfig {
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stderr)
	}
	c.Logger = common.BufferLogger(c.Logger)
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *InstanceRunnerConfig) Validate() error {
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	return nil
}
