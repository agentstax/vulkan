package monitor

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/metrics/datastore"
	"go.opentelemetry.io/otel/metric"
)

// workerGauges owns the otel ObservableGauge instruments for fleet-wide
// worker health -- registered once, RegisterWorkerGauges' caller's concern.
type workerGauges struct {
	monitor            *Monitor
	unclaimedWorkers   metric.Int64ObservableGauge
	oldestUnclaimedAge metric.Int64ObservableGauge
	failingWorkers     metric.Int64ObservableGauge
}

// RegisterWorkerGauges registers the fleet-wide worker-health gauges
// against the monitor's meter. Call once per process -- calling it again
// registers duplicate instruments.
func (m *Monitor) RegisterWorkerGauges() error {
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

	g := &workerGauges{
		monitor:            m,
		unclaimedWorkers:   unclaimedWorkers,
		oldestUnclaimedAge: oldestUnclaimedAge,
		failingWorkers:     failingWorkers,
	}

	_, err = m.meter.RegisterCallback(g.observe, unclaimedWorkers, oldestUnclaimedAge, failingWorkers)
	return err
}

// observe is the callback behind all three worker gauges -- one
// WorkerSnapshots call per collection cycle feeds them, not one query per
// instrument.
func (g *workerGauges) observe(ctx context.Context, o metric.Observer) error {
	workers, err := g.monitor.Datastore.WorkerSnapshots(ctx)
	if err != nil {
		return err
	}

	var unclaimed, failing int64
	var oldest time.Duration
	for _, worker := range workers {
		if worker.Status == datastore.WorkerUnclaimed {
			unclaimed++
			if worker.UnclaimedFor > oldest {
				oldest = worker.UnclaimedFor
			}
		}
		if worker.Attempts > 0 {
			failing++
		}
	}

	o.ObserveInt64(g.unclaimedWorkers, unclaimed)
	o.ObserveInt64(g.oldestUnclaimedAge, oldest.Milliseconds())
	o.ObserveInt64(g.failingWorkers, failing)

	return nil
}
