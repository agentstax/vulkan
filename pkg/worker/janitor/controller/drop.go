package controller

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// DropExpiredPartitions drops each surviving partition whose newest row is
// past ttl, skipping the active partition and (unless overridden) anything a
// lagging group hasn't committed past yet. ttl <= 0 means retention is
// disabled -- the call is a no-op.
func (c *JanitorController) DropExpiredPartitions(ctx context.Context, topicId int64, partitionSize int64, ttl time.Duration, allowDropPastCommitted bool, deliveryLogMode topic.DeliveryLogMode) error {
	if topicId <= 0 {
		return errors.New("topicId must be > 0")
	}
	if partitionSize <= 0 {
		return errors.New("partitionSize must be > 0")
	}

	return c.datastore.DropExpiredPartitions(ctx, topicId, partitionSize, ttl, allowDropPastCommitted, deliveryLogMode)
}
