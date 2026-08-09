package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// eventKey is the (message, attempt) identity an abandoned event and its
// matching cleared event share -- topicId/group are already fixed by the
// routing key both reads filter on, so they're not part of the key.
type eventKey struct {
	MessageId int64
	Attempt   int
}

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
	abandoned, err := c.datastore.EventTimestamps(ctx, routingKey, "abandoned")
	if err != nil {
		return nil, err
	}
	cleared, err := c.datastore.EventTimestamps(ctx, routingKey, "cleared")
	if err != nil {
		return nil, err
	}

	clearedAt := make(map[eventKey]time.Time, len(cleared))
	for _, event := range cleared {
		clearedAt[eventKey{MessageId: event.MessageId, Attempt: event.Attempt}] = event.At
	}

	var snapshot metrics.AbandonedRoutineSnapshot
	var latencySum time.Duration
	var matched int64
	for _, event := range abandoned {
		snapshot.Total++
		at, ok := clearedAt[eventKey{MessageId: event.MessageId, Attempt: event.Attempt}]
		if !ok {
			snapshot.Outstanding++
			continue
		}
		latencySum += at.Sub(event.At)
		matched++
	}
	if matched > 0 {
		snapshot.SelfClearLatencyAvg = latencySum / time.Duration(matched)
	}

	return &snapshot, nil
}
