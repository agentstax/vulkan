package metrics

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/metrics/controller"
	"go.opentelemetry.io/otel/metric"
)

// dutyMetric owns the otel ObservableGauge instruments for fleet-wide duty
// health -- registered once, RegisterDutyMetric's caller's concern.
type dutyMetric struct {
	controller    *controller.MetricsController
	overdueDuties metric.Int64ObservableGauge
	oldestGateAge metric.Int64ObservableGauge
	failingDuties metric.Int64ObservableGauge
}

// RegisterDutyMetric registers the fleet-wide duty-health gauges against the
// meter. Call once per process (FleetMaintainer.Register) -- calling it
// again registers duplicate instruments.
func (m *Metrics) RegisterDutyMetric() error {
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

	failingDuties, err := m.meter.Int64ObservableGauge(
		"vulkan.maintain.duty_state.failing_duties",
		metric.WithDescription("Duties with a nonzero failure streak -- erroring and backed off, independent of overdue."),
		metric.WithUnit("{duty}"),
	)
	if err != nil {
		return err
	}

	d := &dutyMetric{
		controller:    m.controller,
		overdueDuties: overdueDuties,
		oldestGateAge: oldestGateAge,
		failingDuties: failingDuties,
	}

	_, err = m.meter.RegisterCallback(d.observe, overdueDuties, oldestGateAge, failingDuties)
	return err
}

// observe is the callback behind all three duty gauges -- one DutySnapshots call
// per collection cycle feeds them, not one query per instrument.
func (d *dutyMetric) observe(ctx context.Context, o metric.Observer) error {
	duties, err := d.controller.DutySnapshots(ctx)
	if err != nil {
		return err
	}

	var overdue, failing int64
	var oldest time.Duration
	for i, duty := range duties {
		if duty.Overdue {
			overdue++
		}
		if duty.Attempts > 0 {
			failing++
		}
		if i == 0 || duty.GateAge > oldest {
			oldest = duty.GateAge
		}
	}

	o.ObserveInt64(d.overdueDuties, overdue)
	o.ObserveInt64(d.oldestGateAge, oldest.Milliseconds())
	o.ObserveInt64(d.failingDuties, failing)

	return nil
}
