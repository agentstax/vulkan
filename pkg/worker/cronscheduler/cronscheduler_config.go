package cronscheduler

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

type CronSchedulerConfig struct {
	// InstanceTTL is how long the claimed worker_instance row stays live
	// without a renewal -- past it the instance counts as dead and a
	// replacement can claim. The heartbeat renews at half this.
	// Default: 30s.
	InstanceTTL time.Duration

	// JitterFraction spreads scan ticks out of phase: each tick's delay is
	// poll_rate * (1 ± JitterFraction), and the first tick is uniform over
	// one whole interval.
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	Logger    common.Logger       // pass your own *slog.Logger (own Handler) or anything satisfying common.Logger. Default: text logger to stdout, warn level and up.
	Retry     *common.RetryPolicy // transient-error retry policy for the cron scheduler's own Postgres calls. Default: common.NewDefaultRetryPolicy().
	ScanRetry *common.RetryPolicy // failed-scan backoff curve, unrelated to Retry above. Default: common.NewDefaultRetryPolicy().
}

func (c *CronSchedulerConfig) WithDefaults() *CronSchedulerConfig {
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	c.ScanRetry = c.ScanRetry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *CronSchedulerConfig) Validate() error {
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	if c.JitterFraction < 0 || c.JitterFraction >= 1 {
		return fmt.Errorf("JitterFraction must be in [0, 1), got %v", c.JitterFraction)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	if err := c.ScanRetry.Validate(); err != nil {
		return fmt.Errorf("ScanRetry: %w", err)
	}
	return nil
}
