package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
)

// warnPartitions is where one never-superseded key's replay, at ~10µs per
// partition, crosses ~100ms.
const warnPartitions = 10_000

// Evaluate measures the owner's topic and returns its alert, nil when none
// applies. threshold 0 uses the default warnPartitions.
func (c *CompactionReadCostController) Evaluate(ctx context.Context, owner *common.Owner, threshold int64) (*alert.Alert, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if threshold < 0 {
		return nil, fmt.Errorf("threshold must be >= 0, got %d", threshold)
	}
	if threshold == 0 {
		threshold = warnPartitions
	}

	compacted, err := c.datastore.Compacted(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	// only compacted topics carry a read cost
	if !compacted {
		return nil, nil
	}

	count, err := c.datastore.PartitionCount(ctx, owner.TopicId)
	if err != nil {
		return nil, err
	}
	if count < threshold {
		return nil, nil
	}
	return newCompactionReadCostAlert(owner, count, threshold)
}
