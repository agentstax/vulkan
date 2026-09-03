package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

// GetCompactionHeadInTx reads the head against the caller's tx, locking it
// FOR UPDATE so a following produce on the same key is a race-free
// compare-and-set.
func (c *ProducerController) GetCompactionHeadInTx[Message common.Versioned](ctx context.Context, tx iDatastore.Tx, topicId int64, messageKey string) (*common.MessageData[Message], error) {
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
	return toMessageData[Message](data)
}
