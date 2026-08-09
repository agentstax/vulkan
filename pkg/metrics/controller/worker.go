package controller

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// WorkerSnapshots is every worker row's current claim state.
func (c *MetricsController) WorkerSnapshots(ctx context.Context) ([]metrics.WorkerSnapshot, error) {
	data, err := c.datastore.WorkerSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	workers := make([]metrics.WorkerSnapshot, 0, len(data))
	for _, row := range data {
		snapshot, err := toWorkerSnapshot(row)
		if err != nil {
			return nil, err
		}
		workers = append(workers, snapshot)
	}
	return workers, nil
}
