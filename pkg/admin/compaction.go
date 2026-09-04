package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// GetCompactionHead returns messageKey's current compaction head, or
// (nil, nil) if nothing has been produced under it.
// Returns ErrTopicNotFound when the topic isn't registered.
func (a *MessageAdmin) GetCompactionHead[Message common.Versioned](ctx context.Context, topicName string, messageKey string) (*common.Message[Message], error) {
	found, err := a.GetTopic(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}
	return a.heads.GetHead[Message](ctx, found.Id, messageKey)
}

// ListKeyMessages returns messageKey's retained messages, newest first.
// Returns ErrTopicNotFound when the topic isn't registered.
func (a *MessageAdmin) ListKeyMessages[Message common.Versioned](ctx context.Context, topicName string, messageKey string, limit int) ([]*common.Message[Message], error) {
	found, err := a.GetTopic(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}
	return a.heads.ListKeyMessages[Message](ctx, found.Id, messageKey, limit)
}
