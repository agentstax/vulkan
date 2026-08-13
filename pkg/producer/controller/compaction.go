package controller

import (
	"context"
	"errors"
)

// GetCompactionHeadInTx reads the head against the caller's tx, locking it
// FOR UPDATE so a following produce on the same key is a race-free
// compare-and-set.
func (c *ProducerController[Message]) GetCompactionHeadInTx(ctx context.Context, tx Tx, topicId int64, compactionKey string) (*MessageRow[Message], error) {
	if tx == nil {
		return nil, errors.New("tx must not be nil")
	}
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if compactionKey == "" {
		return nil, errors.New("compaction key is required")
	}

	data, err := c.datastore.GetCompactionHeadInTx(ctx, tx.Raw(), topicId, compactionKey)
	if err != nil || data == nil {
		return nil, err
	}
	return toMessageRow[Message](data)
}
