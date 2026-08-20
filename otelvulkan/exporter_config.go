package otelvulkan

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
)

type ExporterConfig struct {
	// CollectTimeout bounds the Postgres reads a scrape drives -- the
	// instrument registration pass and the observation callback. A scrape
	// request may carry no deadline of its own.
	// Default: 5s.
	CollectTimeout time.Duration

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for the Postgres reads. Default: common.NewDefaultRetryPolicy().
}

func (c *ExporterConfig) WithDefaults() *ExporterConfig {
	if c.CollectTimeout == 0 {
		c.CollectTimeout = 5 * time.Second
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
func (c *ExporterConfig) Validate() error {
	if c.CollectTimeout <= 0 {
		return fmt.Errorf("CollectTimeout must be > 0, got %v", c.CollectTimeout)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
