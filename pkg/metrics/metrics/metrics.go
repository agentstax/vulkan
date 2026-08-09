package metrics

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/metrics/controller"
	"go.opentelemetry.io/otel/metric"
)

// Metrics owns every otel gauge registration derived from the metrics
// controller's snapshots, so callers stop hand-rolling their own meter
// wiring.
type Metrics struct {
	controller *controller.MetricsController
	meter      metric.Meter
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMetrics(metricsController *controller.MetricsController, cfg *MetricsConfig) (*Metrics, error) {
	if metricsController == nil {
		return nil, errors.New("metricsController must not be nil")
	}
	if cfg == nil {
		cfg = &MetricsConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Metrics{
		controller: metricsController,
		meter:      cfg.Meter,
	}, nil
}
