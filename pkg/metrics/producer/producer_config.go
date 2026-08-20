package producer

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
)

type ProducerConfig struct {
	Logger common.Logger       // pass your own *slog.Logger or anything satisfying common.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for the producer's own Postgres calls. Default: common.NewDefaultRetryPolicy().
}

func (c *ProducerConfig) WithDefaults() *ProducerConfig {
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stderr)
	}
	c.Logger = common.BufferLogger(c.Logger)
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ProducerConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
