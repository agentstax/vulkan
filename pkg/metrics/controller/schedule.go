package controller

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// ScheduleSnapshots is every schedule row's current schedule health.
func (c *MetricsController) ScheduleSnapshots(ctx context.Context) ([]metrics.ScheduleSnapshot, error) {
	data, err := c.datastore.ScheduleSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	schedules := make([]metrics.ScheduleSnapshot, 0, len(data))
	for _, row := range data {
		snapshot, err := toScheduleSnapshot(row)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, snapshot)
	}
	return schedules, nil
}
