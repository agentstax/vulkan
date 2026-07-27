package monitor

import (
	"github.com/agentstax/vulkan/pkg/datastore"
	metricsDatastore "github.com/agentstax/vulkan/pkg/metrics/datastore"
	"go.opentelemetry.io/otel/metric"
)

// Monitor is the single top-level read surface for the DB-snapshot metrics:
// it owns MetricsDatastore plus every otel gauge registration derived from
// it, so callers stop hand-rolling their own meter wiring.
type Monitor struct {
	Datastore *metricsDatastore.MetricsDatastore
	meter     metric.Meter
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMonitor(ds *datastore.PostgresDatastore, cfg *MonitorConfig) (*Monitor, error) {
	if cfg == nil {
		cfg = &MonitorConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metricsDatastore, err := metricsDatastore.NewMetricsDatastore(ds, &metricsDatastore.MetricsDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Monitor{
		Datastore: metricsDatastore,
		meter:     cfg.Meter,
	}, nil
}
