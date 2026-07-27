package monitor

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// dutyGauges owns the otel ObservableGauge instruments for fleet-wide duty
// health -- registered once, RegisterDutyGauges' caller's concern.
type dutyGauges struct {
	monitor       *Monitor
	overdueDuties metric.Int64ObservableGauge
	oldestGateAge metric.Int64ObservableGauge
}

// RegisterDutyGauges registers the fleet-wide duty-health gauges against the
// monitor's meter. Call once per process (FleetMaintainer.Register) --
// calling it again registers duplicate instruments.
func (m *Monitor) RegisterDutyGauges() error {
	overdueDuties, err := m.meter.Int64ObservableGauge(
		"vulkan.maintain.duty_state.overdue_duties",
		metric.WithDescription("Duties whose gate trails now() by more than 10x their own rate -- nobody is maintaining them."),
		metric.WithUnit("{duty}"),
	)
	if err != nil {
		return err
	}

	oldestGateAge, err := m.meter.Int64ObservableGauge(
		"vulkan.maintain.duty_state.oldest_gate_age",
		metric.WithDescription("Largest now() - can_run_after across all duties; at or below 0 while every claim is healthy."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	g := &dutyGauges{
		monitor:       m,
		overdueDuties: overdueDuties,
		oldestGateAge: oldestGateAge,
	}

	_, err = m.meter.RegisterCallback(g.observe, overdueDuties, oldestGateAge)
	return err
}

// observe is the callback behind both duty gauges -- one DutySnapshots call
// per collection cycle feeds them, not one query per instrument.
func (g *dutyGauges) observe(ctx context.Context, o metric.Observer) error {
	duties, err := g.monitor.Datastore.DutySnapshots(ctx)
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

	o.ObserveInt64(g.overdueDuties, overdue)
	o.ObserveInt64(g.oldestGateAge, oldest.Milliseconds())

	return nil
}
