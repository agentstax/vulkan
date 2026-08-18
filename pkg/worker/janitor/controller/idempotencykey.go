package controller

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SweepExpiredIdempotencyKeys deletes idempotency claims older than ttl in
// batches.
func (c *JanitorController) SweepExpiredIdempotencyKeys(ctx context.Context, topicId int64, ttl time.Duration, batchSize int) error {
	if topicId <= 0 {
		return errors.New("topicId must be > 0")
	}
	if batchSize <= 0 {
		return fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}

	return c.datastore.SweepExpiredIdempotencyKeys(ctx, topicId, ttl, batchSize)
}
