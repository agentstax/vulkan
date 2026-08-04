package manager

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
)

type RunnerConfig struct {
	// RetryDelay is the pause between claim lives. Keep it >= the provisioner's
	// InstanceTTL -- anything shorter re-claims a row whose live instances
	// haven't expired yet.
	// Default: 30s, matching ManagerConfig.InstanceTTL's default.
	RetryDelay time.Duration

	// JitterFraction spreads retries out of phase across replicas that lost
	// their claims together: each delay is RetryDelay * (1 ± JitterFraction).
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
}

func (c *RunnerConfig) WithDefaults() *RunnerConfig {
	if c.RetryDelay == 0 {
		c.RetryDelay = 30 * time.Second
	}
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *RunnerConfig) Validate() error {
	if c.RetryDelay <= 0 {
		return fmt.Errorf("RetryDelay must be > 0, got %v", c.RetryDelay)
	}
	if c.JitterFraction < 0 || c.JitterFraction >= 1 {
		return fmt.Errorf("JitterFraction must be in [0, 1), got %v", c.JitterFraction)
	}
	return nil
}
