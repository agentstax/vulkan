package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// overdueFactor: how many rates past its gate a duty counts as overdue.
const overdueFactor = 10

// DutySnapshot is one maintenance row's health.
type DutySnapshot struct {
	Duty          string
	TopicName     string
	ConsumerGroup string
	Rate          time.Duration
	GateAge       time.Duration // now() - can_run_after: negative while claimed into the future, positive once eligible and unclaimed
	Overdue       bool          // GateAge > overdueFactor * Rate -- nobody is maintaining this duty (or its owner is stuck)
}

// DutyState owns the otel ObservableGauge instruments for fleet-wide duty health.
type DutyState struct {
	datastore *maintenanceMetricsDatastore

	// otel instruments
	overdueDuties metric.Int64ObservableGauge
	oldestGateAge metric.Int64ObservableGauge
}

func NewDutyState(meter metric.Meter, ds *maintenanceMetricsDatastore) (*DutyState, error) {
	overdueDuties, err := meter.Int64ObservableGauge(
		"vulkan.maintain.duty_state.overdue_duties",
		metric.WithDescription("Duties whose gate trails now() by more than 10x their own rate -- nobody is maintaining them."),
		metric.WithUnit("{duty}"),
	)
	if err != nil {
		return nil, err
	}

	oldestGateAge, err := meter.Int64ObservableGauge(
		"vulkan.maintain.duty_state.oldest_gate_age",
		metric.WithDescription("Largest now() - can_run_after across all duties; at or below 0 while every claim is healthy."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	d := &DutyState{
		datastore: ds,

		overdueDuties: overdueDuties,
		oldestGateAge: oldestGateAge,
	}

	if _, err := meter.RegisterCallback(d.observe,
		overdueDuties,
		oldestGateAge,
	); err != nil {
		return nil, err
	}

	return d, nil
}

// observe is the callback behind both gauges above -- one Snapshot call per
// collection cycle feeds them, not one query per instrument.
func (d *DutyState) observe(ctx context.Context, o metric.Observer) error {
	duties, err := d.Snapshot(ctx)
	if err != nil {
		return err
	}

	var overdue int64
	var oldest time.Duration
	for i, duty := range duties {
		if duty.Overdue {
			overdue++
		}
		if i == 0 || duty.GateAge > oldest {
			oldest = duty.GateAge
		}
	}

	o.ObserveInt64(d.overdueDuties, overdue)
	o.ObserveInt64(d.oldestGateAge, oldest.Milliseconds())

	return nil
}

// Snapshot is every duty's current health, queried live from Postgres --
// works with no otel exporter/backend attached, same data observe reports.
func (d *DutyState) Snapshot(ctx context.Context) ([]DutySnapshot, error) {
	return d.datastore.DutySnapshots(ctx)
}
