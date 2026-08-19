package controller

import (
	"context"
	"errors"
)

// AdvanceCommitted rolls committed forward to the earliest unresolved work
// (open lease, unresolved delivery, or claimed), returning where committed
// landed. committed only ever moves forward.
func (c *CursorAdvancerController) AdvanceCommitted(ctx context.Context, topicId int64, groupId int64) (int64, error) {
	if topicId <= 0 {
		return 0, errors.New("topicId must be > 0")
	}
	if groupId <= 0 {
		return 0, errors.New("groupId must be > 0")
	}

	return c.datastore.AdvanceCommitted(ctx, topicId, groupId)
}
