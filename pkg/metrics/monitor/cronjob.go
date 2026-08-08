package monitor

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// cronJobGauges owns the otel ObservableGauge instruments for fleet-wide
// cron-job health -- registered once, RegisterCronJobGauges' caller's concern.
type cronJobGauges struct {
	monitor       *Monitor
	overdueJobs   metric.Int64ObservableGauge
	oldestDueAge  metric.Int64ObservableGauge
	suspendedJobs metric.Int64ObservableGauge
}

// RegisterCronJobGauges registers the fleet-wide cron-job-health gauges
// against the monitor's meter. Call once per process -- calling it again
// registers duplicate instruments.
func (m *Monitor) RegisterCronJobGauges() error {
	overdueJobs, err := m.meter.Int64ObservableGauge(
		"vulkan.cron.state.overdue_jobs",
		metric.WithDescription("Unsuspended jobs due for longer than the overdue threshold -- nothing is firing them."),
		metric.WithUnit("{job}"),
	)
	if err != nil {
		return err
	}

	oldestDueAge, err := m.meter.Int64ObservableGauge(
		"vulkan.cron.state.oldest_due_age",
		metric.WithDescription("Largest now() - next_scheduled_time across unsuspended due jobs; 0 while every job fires on time."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	suspendedJobs, err := m.meter.Int64ObservableGauge(
		"vulkan.cron.state.suspended_jobs",
		metric.WithDescription("Jobs with suspended = true -- deliberately not firing, excluded from the overdue count."),
		metric.WithUnit("{job}"),
	)
	if err != nil {
		return err
	}

	g := &cronJobGauges{
		monitor:       m,
		overdueJobs:   overdueJobs,
		oldestDueAge:  oldestDueAge,
		suspendedJobs: suspendedJobs,
	}

	_, err = m.meter.RegisterCallback(g.observe, overdueJobs, oldestDueAge, suspendedJobs)
	return err
}

// observe is the callback behind all three cron-job gauges -- one
// CronJobSnapshots call per collection cycle feeds them, not one query per
// instrument.
func (g *cronJobGauges) observe(ctx context.Context, o metric.Observer) error {
	jobs, err := g.monitor.Datastore.CronJobSnapshots(ctx)
	if err != nil {
		return err
	}

	var overdue, suspended int64
	var oldest time.Duration
	for _, job := range jobs {
		if job.Suspended {
			suspended++
			continue
		}
		if job.Overdue {
			overdue++
		}
		if job.DueFor > oldest {
			oldest = job.DueFor
		}
	}

	o.ObserveInt64(g.overdueJobs, overdue)
	o.ObserveInt64(g.oldestDueAge, oldest.Milliseconds())
	o.ObserveInt64(g.suspendedJobs, suspended)

	return nil
}
