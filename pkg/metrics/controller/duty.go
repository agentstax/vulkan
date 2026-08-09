package controller

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// DutySnapshots is every duty's current health.
func (c *MetricsController) DutySnapshots(ctx context.Context) ([]metrics.DutySnapshot, error) {
	data, err := c.datastore.DutySnapshots(ctx)
	if err != nil {
		return nil, err
	}

	duties := make([]metrics.DutySnapshot, 0, len(data))
	for _, row := range data {
		if !row.RateNs.Valid {
			// a row with no poll_rate can't have an Overdue verdict -- skip it
			// rather than fail the whole snapshot
			continue
		}
		duties = append(duties, toDutySnapshot(row))
	}
	return duties, nil
}
