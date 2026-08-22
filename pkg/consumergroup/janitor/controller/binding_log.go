package controller

import (
	"context"
	"fmt"
	"time"
)

// SweepExpiredWaitingDeclarations deletes waiting binding_log rows older than
// ttl, at most batchSize per topic table, keeping each declarer's newest
// waiting row. Returns how many rows were deleted in total.
func (c *JanitorController) SweepExpiredWaitingDeclarations(ctx context.Context, ttl time.Duration, batchSize int) (int64, error) {
	if ttl <= 0 {
		return 0, fmt.Errorf("ttl must be > 0, got %v", ttl)
	}
	if batchSize <= 0 {
		return 0, fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}

	return c.datastore.SweepExpiredWaitingDeclarations(ctx, ttl, batchSize)
}
