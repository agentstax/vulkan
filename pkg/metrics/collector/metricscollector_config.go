package collector

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
)

type MetricsCollectorConfig struct {
	// InstanceTTL is how long the claimed worker_instance row stays live
	// without a renewal -- past it the instance counts as dead and a
	// replacement can claim. The heartbeat renews at half this.
	// Default: 30s.
	InstanceTTL time.Duration

	// JitterFraction spreads collection ticks out of phase: each tick's delay
	// is poll_rate * (1 ± JitterFraction).
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	// TopicConcurrency caps how many topics one collection pass snapshots
	// and produces at once. The collector shares its connection pool with
	// the process embedding it, so the cap is what keeps a large topic
	// count from crowding out that process's own traffic.
	// Default: 4.
	TopicConcurrency int

	Logger       logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry        *common.RetryPolicy // transient-error retry policy for the collector's own Postgres calls. Default: common.NewDefaultRetryPolicy().
	CollectRetry *common.RetryPolicy // failed-collection backoff curve, unrelated to Retry above. Default: common.NewDefaultRetryPolicy().
}

func (c *MetricsCollectorConfig) WithDefaults() *MetricsCollectorConfig {
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.TopicConcurrency == 0 {
		c.TopicConcurrency = 4
	}
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})
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
	if c.TopicConcurrency < 1 {
		return fmt.Errorf("TopicConcurrency must be >= 1, got %d", c.TopicConcurrency)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	if err := c.CollectRetry.Validate(); err != nil {
		return fmt.Errorf("CollectRetry: %w", err)
	}
	return nil
}
