package monitor

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type MonitorConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // Default: retry.NewDefaultRetryPolicy(). Metric polling may want a shorter policy than the default.
	Meter  metric.Meter  // feeds every gauge the monitor registers. Default: a noop meter.
}

func (c *MonitorConfig) WithDefaults() *MonitorConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	if c.Meter == nil {
		// metric/noop, not the global otel.GetMeterProvider() -- reading the
		// global registry requires the top-level otel package, which brings
		// the trace/baggage/go-logr dependencies ie bloat.
		c.Meter = noop.NewMeterProvider().Meter("github.com/agentstax/vulkan/pkg/metrics")
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MonitorConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
