package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// Evaluate returns the owner topic's alert, nil when every worker row it owns
// is claimed or suspended. threshold is unused: the manager deletes expired
// instance rows on every tick, so how long a row has been unclaimed is not
// readable once one runs.
func (c *WorkerLivenessController) Evaluate(ctx context.Context, owner *common.Owner, threshold int64) (*alert.Alert, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if threshold < 0 {
		return nil, fmt.Errorf("threshold must be >= 0, got %d", threshold)
	}

	snapshots, err := c.metrics.WorkerSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	// a group's rows carry the group as owner and resolve to its topic
	var unclaimed []metrics.WorkerSnapshot
	for _, snapshot := range snapshots {
		if snapshot.Owner.TopicId == owner.TopicId && snapshot.Status == metrics.WorkerUnclaimed {
			unclaimed = append(unclaimed, snapshot)
		}
	}
	if len(unclaimed) == 0 {
		return nil, nil
	}
	return newWorkerLivenessAlert(owner, unclaimed)
}
