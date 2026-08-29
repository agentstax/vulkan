package controller

import (
	"context"
	"errors"
	"fmt"
)

// GetCompactionHeadInTx reads the head against the caller's tx, locking it
// FOR UPDATE so a following produce on the same key is a race-free
// compare-and-set.
func (c *ProducerController[Message]) GetCompactionHeadInTx(ctx context.Context, tx Tx, topicId int64, messageKey string) (*MessageRow[Message], error) {
	if tx == nil {
		return nil, errors.New("tx must not be nil")
	}
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if messageKey == "" {
		return nil, errors.New("messageKey must not be empty")
	}

	data, err := c.datastore.GetCompactionHeadInTx(ctx, tx, topicId, messageKey)
	if err != nil || data == nil {
		return nil, err
	}
	return toMessageRow[Message](data)
}
