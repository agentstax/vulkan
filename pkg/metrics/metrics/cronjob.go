package metrics

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/metrics/controller"
	"go.opentelemetry.io/otel/metric"
)

// cronJobMetric owns the otel ObservableGauge instruments for fleet-wide
// cron-job health -- registered once, RegisterCronJobMetric's caller's
// concern.
type cronJobMetric struct {
	controller    *controller.MetricsController
	overdueJobs   metric.Int64ObservableGauge
	oldestDueAge  metric.Int64ObservableGauge
	suspendedJobs metric.Int64ObservableGauge
}

// RegisterCronJobMetric registers the fleet-wide cron-job-health gauges
// against the meter. Call once per process -- calling it again registers
// duplicate instruments.
func (m *Metrics) RegisterCronJobMetric() error {
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

	c := &cronJobMetric{
		controller:    m.controller,
		overdueJobs:   overdueJobs,
		oldestDueAge:  oldestDueAge,
		suspendedJobs: suspendedJobs,
	}

	_, err = m.meter.RegisterCallback(c.observe, overdueJobs, oldestDueAge, suspendedJobs)
	return err
}

// observe is the callback behind all three cron-job gauges -- one
// CronJobSnapshots call per collection cycle feeds them, not one query per
// instrument.
func (c *cronJobMetric) observe(ctx context.Context, o metric.Observer) error {
	jobs, err := c.controller.CronJobSnapshots(ctx)
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

	o.ObserveInt64(c.overdueJobs, overdue)
	o.ObserveInt64(c.oldestDueAge, oldest.Milliseconds())
	o.ObserveInt64(c.suspendedJobs, suspended)

	return nil
}
