package producer

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
)

type ProducerConfig struct {
	SessionFlushRate time.Duration // pace of Run's flush tick -- queued abandoned events and changed session counters land once per tick. Default: 30s.

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for the producer's own Postgres calls. Default: common.NewDefaultRetryPolicy().
}

func (c *ProducerConfig) WithDefaults() *ProducerConfig {
	if c.SessionFlushRate == 0 {
		c.SessionFlushRate = 30 * time.Second
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
func (c *ProducerConfig) Validate() error {
	if c.SessionFlushRate <= 0 {
		return fmt.Errorf("SessionFlushRate must be > 0, got %v", c.SessionFlushRate)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
