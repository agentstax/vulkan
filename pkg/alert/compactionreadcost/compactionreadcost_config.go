package compactionreadcost

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
)

type CompactionReadCostConfig struct {
	// InstanceTTL - how long the claimed worker_instance row stays live
	// between heartbeats.
	// Default: 30s.
	InstanceTTL time.Duration

	// RepeatInterval - how long an active alert stays quiet before it
	// repeats as a reminder.
	// Default: 4h.
	RepeatInterval time.Duration

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for the provisioner's own Postgres calls. Default: common.NewDefaultRetryPolicy().
}

func (c *CompactionReadCostConfig) WithDefaults() *CompactionReadCostConfig {
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}
	if c.RepeatInterval == 0 {
		c.RepeatInterval = 4 * time.Hour
	}
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.BufferLogger(c.Logger)
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *CompactionReadCostConfig) Validate() error {
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	if c.RepeatInterval <= 0 {
		return fmt.Errorf("RepeatInterval must be > 0, got %v", c.RepeatInterval)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
