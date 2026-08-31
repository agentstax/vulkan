package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// GetHead returns the current compaction head under messageKey,
// or nil if nothing has been published under it.
func (c *CompactionController) GetHead[Message topic.Versioned](ctx context.Context, topicId int64, messageKey string) (*common.MessageData[Message], error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if messageKey == "" {
		return nil, errors.New("messageKey must not be empty")
	}

	data, err := c.datastore.GetHead(ctx, topicId, messageKey)
	if err != nil || data == nil {
		return nil, err
	}
	return toMessageData[Message](data)
}

// ListHeads returns every key's current head on the topic, ordered
// by message key.
func (c *CompactionController) ListHeads[Message topic.Versioned](ctx context.Context, topicId int64) ([]*common.MessageData[Message], error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}

	data, err := c.datastore.ListHeads(ctx, topicId)
	if err != nil {
		return nil, err
	}

	heads := make([]*common.MessageData[Message], 0, len(data))
	for i := range data {
		head, err := toMessageData[Message](&data[i])
		if err != nil {
			return nil, err
		}
		heads = append(heads, head)
	}
	return heads, nil
}
