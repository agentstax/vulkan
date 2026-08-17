package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// AbandonedRoutineSnapshot pairs the abandoned/cleared events for (topicId,
// group) read directly off __system.metrics's own message log.
func (c *MetricsController) AbandonedRoutineSnapshot(ctx context.Context, topicId int64, group string) (*metrics.AbandonedRoutineSnapshot, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if group == "" {
		return nil, errors.New("group is required")
	}

	// one read per event type keeps each query's intent obvious instead of
	// one query encoding both via CASE/HAVING
	routingKey := metrics.AbandonedRoutineKey(topicId, group)
	abandoned, err := c.datastore.EventTimestamps(ctx, routingKey, metrics.EventAbandoned)
	if err != nil {
		return nil, err
	}
	cleared, err := c.datastore.EventTimestamps(ctx, routingKey, metrics.EventCleared)
	if err != nil {
		return nil, err
	}

	return toAbandonedRoutineSnapshot(abandoned, cleared), nil
}
