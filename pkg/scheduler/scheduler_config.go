package scheduler

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
)

type SchedulerConfig struct {
	// Timeout - how long one message's delivery may run.
	// Default: 30s.
	Timeout time.Duration

	// Concurrency - whether a message runs while a previous one is still
	// running (parallel) or waits for it (exclusive).
	// Default: parallel.
	Concurrency common.ConcurrencyPolicy

	// Metadata - marshaled to opaque JSON stored on the row and shown by
	// `vulkan schedule get`; it is not part of the produced message.
	// Default: {}.
	Metadata any

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for this scheduler's own Postgres calls. Default: common.NewDefaultRetryPolicy().
}

func (c *SchedulerConfig) WithDefaults() *SchedulerConfig {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.Concurrency == "" {
		c.Concurrency = common.ConcurrencyParallel
	}
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *SchedulerConfig) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("Timeout must be > 0, got %v", c.Timeout)
	}
	if err := c.Concurrency.Validate(); err != nil {
		return fmt.Errorf("Concurrency: %w", err)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
