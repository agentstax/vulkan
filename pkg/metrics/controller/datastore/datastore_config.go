package datastore

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
)

type MetricsDatastoreConfig struct {
	Logger common.Logger       // pass your own *slog.Logger (own Handler) or anything satisfying common.Logger. Default: text logger to stdout, warn level and up.
	Retry  *common.RetryPolicy // Default: common.NewDefaultRetryPolicy(). Metric polling may want a shorter policy than the default.
}

func (c *MetricsDatastoreConfig) WithDefaults() *MetricsDatastoreConfig {
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MetricsDatastoreConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
