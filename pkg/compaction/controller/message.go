package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// ListCompactionKeyMessages returns compactionKey's retained messages, newest
// first. limit is required: an unbounded read spans the whole retention window.
func (c *CompactionController[Message]) ListCompactionKeyMessages(ctx context.Context, topicId int64, compactionKey string, limit int) ([]*common.MessageRow[Message], error) {
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if compactionKey == "" {
		return nil, errors.New("compaction key is required")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be > 0, got %d", limit)
	}

	data, err := c.datastore.ListCompactionKeyMessages(ctx, topicId, compactionKey, limit)
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
