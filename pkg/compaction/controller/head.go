package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// GetCompactionHead returns the current compaction head under compactionKey,
// or nil if nothing has been published under it.
func (c *CompactionController[Message]) GetCompactionHead(ctx context.Context, topicId int64, compactionKey string) (*common.MessageRow[Message], error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if compactionKey == "" {
		return nil, errors.New("compaction key is required")
	}

	data, err := c.datastore.GetCompactionHead(ctx, topicId, compactionKey)
	if err != nil || data == nil {
		return nil, err
	}
	return toMessageRow[Message](data)
}

// ListCompactionHeads returns every key's current head on the topic, ordered
// by compaction key.
func (c *CompactionController[Message]) ListCompactionHeads(ctx context.Context, topicId int64) ([]*common.MessageRow[Message], error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}

	data, err := c.datastore.ListCompactionHeads(ctx, topicId)
	if err != nil {
		return nil, err
	}

	heads := make([]*common.MessageRow[Message], 0, len(data))
	for i := range data {
		head, err := toMessageRow[Message](&data[i])
		if err != nil {
			return nil, err
		}
		heads = append(heads, head)
	}
	return heads, nil
}
