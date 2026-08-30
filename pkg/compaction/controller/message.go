package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// ListKeyMessages returns messageKey's retained messages, newest
// first. limit is required: an unbounded read spans the whole retention window.
func (c *CompactionController) ListKeyMessages[Message topic.Versioned](ctx context.Context, topicId int64, messageKey string, limit int) ([]*common.MessageRow[Message], error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if messageKey == "" {
		return nil, errors.New("messageKey must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be > 0, got %d", limit)
	}

	data, err := c.datastore.ListKeyMessages(ctx, topicId, messageKey, limit)
	if err != nil {
		return nil, err
	}

	messages := make([]*common.MessageRow[Message], 0, len(data))
	for i := range data {
		message, err := toMessageRow[Message](&data[i])
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}
