package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// ConsumerGroupSnapshot is the current picture for (topicId, consumerGroup)
// with every section filled.
func (c *MetricsController) ConsumerGroupSnapshot(ctx context.Context, topicId int64, consumerGroup string) (*metrics.ConsumerGroupSnapshot, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if consumerGroup == "" {
		return nil, errors.New("consumerGroup is required")
	}

	data, err := c.datastore.ConsumerGroupSnapshot(ctx, topicId, consumerGroup)
	if err != nil {
		return nil, err
	}
	abandonedRoutines, err := c.AbandonedRoutineSnapshot(ctx, topicId, consumerGroup)
	if err != nil {
		return nil, err
	}

	snapshot := toConsumerGroupSnapshot(consumerGroup, data)
	snapshot.AbandonedRoutines = *abandonedRoutines
	return snapshot, nil
}

// ListConsumerGroups is every group registered on topicId -- the groups a
// health view must account for before the topic can be considered drained.
func (c *MetricsController) ListConsumerGroups(ctx context.Context, topicId int64) ([]string, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	return c.datastore.ListConsumerGroups(ctx, topicId)
}
