package systemmanager

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
)

type SystemManagerConfig struct {
	// JitterFraction spreads Run's between-life delays out of phase across
	// replicas: each delay is RunRetry's curve value * (1 ± JitterFraction).
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	Logger   logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry    *common.RetryPolicy // transient-error retry policy for Postgres calls. Default: common.NewDefaultRetryPolicy().
	RunRetry *common.RetryPolicy // backoff between reconcile-loop lives after one ends on its own, unrelated to Retry above. Default: common.NewDefaultRetryPolicy().
}

func (c *SystemManagerConfig) WithDefaults() *SystemManagerConfig {
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})
	c.Retry = c.Retry.WithDefaults()
	c.RunRetry = c.RunRetry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *SystemManagerConfig) Validate() error {
	if c.JitterFraction < 0 || c.JitterFraction >= 1 {
		return fmt.Errorf("JitterFraction must be in [0, 1), got %v", c.JitterFraction)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	if err := c.RunRetry.Validate(); err != nil {
		return fmt.Errorf("RunRetry: %w", err)
	}
	return nil
}
