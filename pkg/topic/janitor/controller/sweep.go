package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// SweepExpiredPartitions drains the ttl-expired prefix of every surviving
// partition -- covers the low-volume tail that never fills a partition wide
// enough to earn a whole-partition drop. ttl <= 0 means retention is
// disabled -- the call is a no-op.
func (c *JanitorController) SweepExpiredPartitions(ctx context.Context, topicId int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, batchSize int, deliveryLogMode topic.DeliveryLogMode) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if partitionSize <= 0 {
		return fmt.Errorf("partitionSize must be > 0, got %d", partitionSize)
	}
	if batchSize <= 0 {
		return fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}

	return c.datastore.SweepExpiredPartitions(ctx, topicId, partitionSize, ttl, allowDropPastCommitted, batchSize, deliveryLogMode)
}
