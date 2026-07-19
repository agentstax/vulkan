package metrics

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type ConsumerMetricsDatastoreConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // Default: retry.NewDefaultRetryPolicy(). Metric polling may want a shorter policy than the default.
}

func (c *ConsumerMetricsDatastoreConfig) WithDefaults() *ConsumerMetricsDatastoreConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ConsumerMetricsDatastoreConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
