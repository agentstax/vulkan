package manager

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

type ManagerConfig struct {
	// InstanceTTL is how long the claimed worker_instance row stays live
	// without a renewal -- past it the instance counts as dead and a
	// replacement can claim. The heartbeat renews at half this.
	// Default: 30s.
	InstanceTTL time.Duration

	// JitterFraction spreads discovery ticks out of phase across manager
	// replicas: each tick's delay is the row's poll_rate * (1 ± JitterFraction).
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	Logger       common.Logger       // pass your own *slog.Logger or anything satisfying common.Logger. Default: text lines to stderr, warn level and up.
	Retry        *common.RetryPolicy // transient-error retry policy for the worker datastore's own Postgres calls. Default: common.NewDefaultRetryPolicy().
	RefreshRetry *common.RetryPolicy // failed-refresh backoff curve, unrelated to Retry above. Default: common.NewDefaultRetryPolicy().
}

func (c *ManagerConfig) WithDefaults() *ManagerConfig {
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stderr)
	}
	c.Logger = common.BufferLogger(c.Logger)
	c.Retry = c.Retry.WithDefaults()
	c.RefreshRetry = c.RefreshRetry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ManagerConfig) Validate() error {
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	if c.JitterFraction < 0 || c.JitterFraction >= 1 {
		return fmt.Errorf("JitterFraction must be in [0, 1), got %v", c.JitterFraction)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	if err := c.RefreshRetry.Validate(); err != nil {
		return fmt.Errorf("RefreshRetry: %w", err)
	}
	return nil
}
