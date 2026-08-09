package metrics

import (
	"context"
	"time"

	vulkanmetrics "github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/metrics/controller"
	"go.opentelemetry.io/otel/metric"
)

// workerMetric owns the otel ObservableGauge instruments for fleet-wide
// worker health -- registered once, RegisterWorkerMetric's caller's concern.
type workerMetric struct {
	controller         *controller.MetricsController
	unclaimedWorkers   metric.Int64ObservableGauge
	oldestUnclaimedAge metric.Int64ObservableGauge
	failingWorkers     metric.Int64ObservableGauge
}

// RegisterWorkerMetric registers the fleet-wide worker-health gauges
// against the meter. Call once per process -- calling it again registers
// duplicate instruments.
func (m *Metrics) RegisterWorkerMetric() error {
	unclaimedWorkers, err := m.meter.Int64ObservableGauge(
		"vulkan.worker.state.unclaimed_workers",
		metric.WithDescription("Workers with no live instance and a nonzero target -- nobody is running them."),
		metric.WithUnit("{worker}"),
	)
	if err != nil {
		return err
	}

	oldestUnclaimedAge, err := m.meter.Int64ObservableGauge(
		"vulkan.worker.state.oldest_unclaimed_age",
		metric.WithDescription("Largest now() - newest expires_at across unclaimed workers; 0 while every worker is claimed or suspended."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	failingWorkers, err := m.meter.Int64ObservableGauge(
		"vulkan.worker.state.failing_workers",
		metric.WithDescription("Workers with a live instance on a nonzero failure streak -- erroring and backed off, independent of the unclaimed count."),
		metric.WithUnit("{worker}"),
	)
	if err != nil {
		return err
	}

	w := &workerMetric{
		controller:         m.controller,
		unclaimedWorkers:   unclaimedWorkers,
		oldestUnclaimedAge: oldestUnclaimedAge,
		failingWorkers:     failingWorkers,
	}

	_, err = m.meter.RegisterCallback(w.observe, unclaimedWorkers, oldestUnclaimedAge, failingWorkers)
	return err
}

// observe is the callback behind all three worker gauges -- one
// WorkerSnapshots call per collection cycle feeds them, not one query per
// instrument.
func (w *workerMetric) observe(ctx context.Context, o metric.Observer) error {
	workers, err := w.controller.WorkerSnapshots(ctx)
	if err != nil {
		return err
	}

	var unclaimed, failing int64
	var oldest time.Duration
	for _, worker := range workers {
		if worker.Status == vulkanmetrics.WorkerUnclaimed {
			unclaimed++
			if worker.UnclaimedFor > oldest {
				oldest = worker.UnclaimedFor
			}
		}
		if worker.Attempts > 0 {
			failing++
		}
	}

	o.ObserveInt64(w.unclaimedWorkers, unclaimed)
	o.ObserveInt64(w.oldestUnclaimedAge, oldest.Milliseconds())
	o.ObserveInt64(w.failingWorkers, failing)

	return nil
}
