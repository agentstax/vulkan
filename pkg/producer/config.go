package producer

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type MessageProducerConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // Default: retry.NewDefaultRetryPolicy().

	// BatchMaxSize - messages sharing one batched-Produce transaction. Caps
	// lock-hold, latency tail, and the rerun cost of evicting poison.
	// Default: 100.
	BatchMaxSize int

	// BatchConcurrencyLimit - workers committing a topic's batches at once
	// (one pooled connection each).
	// Default: 4.
	BatchConcurrencyLimit int

	// BatchAttemptTimeout - bound on one batch transaction attempt.
	// Default: 10s.
	BatchAttemptTimeout time.Duration

	// BatchShutdownGrace - how long a cancelled Produce keeps waiting for its
	// real outcome. Keep it above BatchAttemptTimeout.
	// Default: 15s. Negative: abandon immediately.
	BatchShutdownGrace time.Duration

	// DisableGracefulShutdown - lets Register accept a non-cancellable
	// lifecycle context (e.g. context.Background()). For short-lived
	// scripts and jobs only -- a service should pass its shutdown context.
	// Default: false.
	DisableGracefulShutdown bool
}

func (c *MessageProducerConfig) WithDefaults() *MessageProducerConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	if c.BatchMaxSize == 0 {
		c.BatchMaxSize = 100
	}
	if c.BatchConcurrencyLimit == 0 {
		c.BatchConcurrencyLimit = 4
	}
	if c.BatchAttemptTimeout == 0 {
		c.BatchAttemptTimeout = 10 * time.Second
	}
	if c.BatchShutdownGrace == 0 {
		c.BatchShutdownGrace = 15 * time.Second
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MessageProducerConfig) Validate() error {
	if c.BatchMaxSize < 1 {
		return fmt.Errorf("BatchMaxSize must be >= 1, got %d", c.BatchMaxSize)
	}
	if c.BatchConcurrencyLimit < 1 {
		return fmt.Errorf("BatchConcurrencyLimit must be >= 1, got %d", c.BatchConcurrencyLimit)
	}
	if c.BatchAttemptTimeout <= 0 {
		return fmt.Errorf("BatchAttemptTimeout must be > 0, got %v", c.BatchAttemptTimeout)
	}
	// negative grace is meaningful (abandon immediately) -- but a positive grace
	// at or below the attempt timeout gives up right before the outcome arrives
	if c.BatchShutdownGrace > 0 && c.BatchShutdownGrace <= c.BatchAttemptTimeout {
		return fmt.Errorf("BatchShutdownGrace (%v) must be > BatchAttemptTimeout (%v), or negative to abandon immediately", c.BatchShutdownGrace, c.BatchAttemptTimeout)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
