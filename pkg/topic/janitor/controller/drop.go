package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// DropExpiredPartitions drops each surviving partition whose newest row is
// past ttl, skipping the active partition and (unless overridden) anything a
// lagging group hasn't committed past yet. ttl <= 0 means retention is
// disabled -- the call is a no-op.
func (c *JanitorController) DropExpiredPartitions(ctx context.Context, topicId int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, deliveryLogMode topic.DeliveryLogMode) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if partitionSize <= 0 {
		return fmt.Errorf("partitionSize must be > 0, got %d", partitionSize)
	}

	return c.datastore.DropExpiredPartitions(ctx, topicId, partitionSize, ttl, allowDropPastCommitted, deliveryLogMode)
}
