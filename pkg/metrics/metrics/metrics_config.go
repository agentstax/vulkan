package metrics

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type MetricsConfig struct {
	Meter metric.Meter // feeds every gauge registered here. Default: a noop meter.
}

func (c *MetricsConfig) WithDefaults() *MetricsConfig {
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
func (c *MetricsConfig) Validate() error {
	return nil
}
