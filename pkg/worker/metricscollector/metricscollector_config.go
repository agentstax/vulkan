package metricscollector

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type MetricsCollectorConfig struct {
	// InstanceTTL is how long the claimed worker_instance row stays live
	// without a renewal -- past it the instance counts as dead and a
	// replacement can claim. The heartbeat renews at half this.
	// Default: 30s.
	InstanceTTL time.Duration

	// JitterFraction spreads collection ticks out of phase: each tick's delay
	// is poll_rate * (1 ± JitterFraction), and the first tick is uniform over
	// one whole interval.
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	Logger       logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry        *retry.Policy // transient-error retry policy for the collector's own Postgres calls. Default: retry.NewDefaultRetryPolicy().
	CollectRetry *retry.Policy // failed-collection backoff curve, unrelated to Retry above. Default: retry.NewDefaultRetryPolicy().
}

func (c *MetricsCollectorConfig) WithDefaults() *MetricsCollectorConfig {
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	c.CollectRetry = c.CollectRetry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MetricsCollectorConfig) Validate() error {
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	if c.JitterFraction < 0 || c.JitterFraction >= 1 {
		return fmt.Errorf("JitterFraction must be in [0, 1), got %v", c.JitterFraction)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	if err := c.CollectRetry.Validate(); err != nil {
		return fmt.Errorf("CollectRetry: %w", err)
	}
	return nil
}
