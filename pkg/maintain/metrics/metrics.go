package metrics

import (
	"github.com/agentstax/vulkan/pkg/datastore"
	"go.opentelemetry.io/otel/metric"
)

// MaintenanceMetrics is the fleet-level duty-health view -- every duty row's
// gate age across the deployment, not scoped to one topic or group.
type MaintenanceMetrics struct {
	DutyState *DutyState
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMaintenanceMetrics(meter metric.Meter, ds *datastore.PostgresDatastore, cfg *MaintenanceMetricsDatastoreConfig) (*MaintenanceMetrics, error) {
	metricsDatastore, err := NewMaintenanceDatastore(ds, cfg)
	if err != nil {
		return nil, err
	}

	dutyState, err := NewDutyState(meter, metricsDatastore)
	if err != nil {
		return nil, err
	}

	return &MaintenanceMetrics{
		DutyState: dutyState,
	}, nil
}
