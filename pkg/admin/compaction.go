package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/compaction"
	"github.com/agentstax/vulkan/pkg/topic"
)

// GetCompactionHead returns messageKey's current compaction head.
// Returns ErrTopicNotFound when the topic isn't registered and
// ErrCompactionHeadNotFound when no compacted message was produced under
// the key.
func (a *MessageAdmin) GetCompactionHead[Message common.Versioned](ctx context.Context, topicName string, messageKey string) (*common.Message[Message], error) {
	found, err := a.GetTopic(ctx, topicName)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, topic.ErrTopicNotFound.With("topic", topicName)
	}
	head, err := a.heads.GetHead[Message](ctx, found.Id, messageKey)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, compaction.ErrCompactionHeadNotFound.With("topic", topicName, "topic_id", found.Id, "message_key", messageKey)
	}
	return head, nil
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
