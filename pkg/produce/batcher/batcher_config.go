package batcher

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common/logging"
)

type BatcherConfig struct {
	// MaxSize - messages sharing one batched-Produce transaction. Caps
	// lock-hold, latency tail, and the rerun cost of evicting poison.
	// Default: 100.
	MaxSize int

	// ConcurrencyLimit - workers committing a topic's batches at once
	// (one pooled connection each).
	// Default: 4.
	ConcurrencyLimit int

	// AttemptTimeout - bound on one batch transaction attempt.
	// Default: 10s.
	AttemptTimeout time.Duration

	// ShutdownGrace - how long a cancelled Produce keeps waiting for its
	// real outcome. Keep it above AttemptTimeout.
	// Default: 15s. Negative: abandon immediately.
	ShutdownGrace time.Duration

	Logger logging.Logger // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
}

func (c *BatcherConfig) WithDefaults() *BatcherConfig {
	if c.MaxSize == 0 {
		c.MaxSize = 100
	}
	if c.ConcurrencyLimit == 0 {
		c.ConcurrencyLimit = 4
	}
	if c.AttemptTimeout == 0 {
		c.AttemptTimeout = 10 * time.Second
	}
	if c.ShutdownGrace == 0 {
		c.ShutdownGrace = 15 * time.Second
	}
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *BatcherConfig) Validate() error {
	if c.MaxSize < 1 {
		return fmt.Errorf("MaxSize must be >= 1, got %d", c.MaxSize)
	}
	if c.ConcurrencyLimit < 1 {
		return fmt.Errorf("ConcurrencyLimit must be >= 1, got %d", c.ConcurrencyLimit)
	}
	if c.AttemptTimeout <= 0 {
		return fmt.Errorf("AttemptTimeout must be > 0, got %v", c.AttemptTimeout)
	}

	// negative grace is meaningful (abandon immediately) -- but a positive grace
	// at or below the attempt timeout gives up right before the outcome arrives
	if c.ShutdownGrace > 0 && c.ShutdownGrace <= c.AttemptTimeout {
		return fmt.Errorf("ShutdownGrace (%v) must be > AttemptTimeout (%v), or negative to abandon immediately", c.ShutdownGrace, c.AttemptTimeout)
	}
	return nil
}
