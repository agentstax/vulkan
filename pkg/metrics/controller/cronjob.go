package controller

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// CronJobSnapshots is every cron_job row's current firing health.
func (c *MetricsController) CronJobSnapshots(ctx context.Context) ([]metrics.CronJobSnapshot, error) {
	data, err := c.datastore.CronJobSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	jobs := make([]metrics.CronJobSnapshot, 0, len(data))
	for _, row := range data {
		snapshot, err := toCronJobSnapshot(row)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, snapshot)
	}
	return jobs, nil
}
