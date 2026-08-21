package producer

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/producer/batcher"
)

type ProducerConfig struct {
	// Message - this producer's default MessageOptions, merged UNDER every
	// produce: a field the per-produce ProduceOptions.Message leaves unset
	// takes its value from here before the message is stored. Fields unset in
	// both stay unset -- the consumer decides.
	// Default: nil (no producer-side defaults).
	Message *common.MessageOptions

	// Batch - knobs for the shared-transaction batching of concurrent Produce
	// calls. See batcher.BatcherConfig for fields and defaults.
	Batch batcher.BatcherConfig

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for this producer's own Postgres calls -- never put on messages. Default: common.NewDefaultRetryPolicy().
}

func (c *ProducerConfig) WithDefaults() *ProducerConfig {
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})

	// the batcher inherits this producer's logger unless given its own
	if c.Batch.Logger == nil {
		c.Batch.Logger = c.Logger
	}
	c.Batch.WithDefaults()

	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ProducerConfig) Validate() error {
	if err := c.Message.Validate(); err != nil {
		return fmt.Errorf("Message: %w", err)
	}
	if err := c.Batch.Validate(); err != nil {
		return fmt.Errorf("Batch: %w", err)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
