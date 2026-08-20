package controller

import (
	"context"
	"fmt"
)

// AdvanceCommitted rolls committed forward to the earliest unresolved work
// (open lease, unresolved delivery, or claimed), returning where committed
// landed. committed only ever moves forward.
func (c *CursorAdvancerController) AdvanceCommitted(ctx context.Context, topicId int64, groupId int64) (int64, error) {
	if topicId <= 0 {
		return 0, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return 0, fmt.Errorf("groupId must be > 0, got %d", groupId)
	}

	return c.datastore.AdvanceCommitted(ctx, topicId, groupId)
}
