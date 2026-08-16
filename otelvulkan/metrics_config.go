package otelvulkan

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type MetricsConfig struct {
	// Meter receives the sample instruments -- pass one from your own
	// provider to feed your own otel pipeline.
	// Default: the global otel provider's meter.
	Meter metric.Meter

	// CollectTimeout bounds the Postgres reads a collection drives -- the
	// instrument registration pass and the observation callback. A
	// collection may carry no deadline of its own.
	// Default: 5s.
	CollectTimeout time.Duration

	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // transient-error retry policy for the Postgres reads. Default: retry.NewDefaultRetryPolicy().
}

func (c *MetricsConfig) WithDefaults() *MetricsConfig {
	if c.Meter == nil {
		c.Meter = otel.GetMeterProvider().Meter(meterScopeName)
	}
	if c.CollectTimeout == 0 {
		c.CollectTimeout = 5 * time.Second
	}
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MetricsConfig) Validate() error {
	if c.Meter == nil {
		return errors.New("Meter must not be nil")
	}
	if c.CollectTimeout <= 0 {
		return fmt.Errorf("CollectTimeout must be > 0, got %v", c.CollectTimeout)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
