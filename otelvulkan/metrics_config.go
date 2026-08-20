package otelvulkan

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type MetricsConfig struct {
	// Meter receives the metric instruments -- pass one from your own
	// provider to feed your own otel pipeline.
	// Default: the global otel provider's meter.
	Meter metric.Meter

	// CollectTimeout bounds the Postgres reads a collection drives -- the
	// instrument registration pass and the observation callback. A
	// collection may carry no deadline of its own.
	// Default: 5s.
	CollectTimeout time.Duration

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for the Postgres reads. Default: common.NewDefaultRetryPolicy().
}

func (c *MetricsConfig) WithDefaults() *MetricsConfig {
	if c.Meter == nil {
		c.Meter = otel.GetMeterProvider().Meter(meterScopeName)
	}
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
