package controller

import (
	"context"
	"fmt"
)

// SweepExpiredKeyLeases deletes expired key_lease rows in batches.
func (c *JanitorController) SweepExpiredKeyLeases(ctx context.Context, topicId int64, batchSize int) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if batchSize <= 0 {
		return fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}

	return c.datastore.SweepExpiredKeyLeases(ctx, topicId, batchSize)
}
