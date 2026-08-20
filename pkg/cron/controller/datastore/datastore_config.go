package datastore

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
)

type CronJobDatastoreConfig struct {
	Logger common.Logger       // pass your own *slog.Logger or anything satisfying common.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for this datastore's own Postgres calls. Default: common.NewDefaultRetryPolicy().
}

func (c *CronJobDatastoreConfig) WithDefaults() *CronJobDatastoreConfig {
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stderr)
	}
	c.Logger = common.BufferLogger(c.Logger)
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *CronJobDatastoreConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
