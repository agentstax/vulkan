package vulkan

import (
	"context"
)

// KeyHandle is one message key on its topic plus the client, holding no
// row. Every verb resolves the topic when called and returns the not-found
// error itself.
type KeyHandle[Message Versioned] struct {
	topicName  string
	messageKey string
	client     *Client
}

// Key names a message key on this topic. No I/O and no failure -- each verb
// on the handle resolves the topic when called.
func (t *TopicHandle[Message]) Key(messageKey string) *KeyHandle[Message] {
	return &KeyHandle[Message]{topicName: t.name, messageKey: messageKey, client: t.client}
}

func (k *KeyHandle[Message]) MessageKey() string {
	return k.messageKey
}

// CompactionHead returns the key's current compaction head, or
// ErrCompactionHeadNotFound if no compacted message was produced under it.
func (k *KeyHandle[Message]) CompactionHead(ctx context.Context) (*StoredMessage[Message], error) {
	return k.client.admin.GetCompactionHead[Message](ctx, k.topicName, k.messageKey)
}

// Messages returns the key's retained messages, newest first.
func (k *KeyHandle[Message]) Messages(ctx context.Context, limit int) ([]*StoredMessage[Message], error) {
	return k.client.admin.ListKeyMessages[Message](ctx, k.topicName, k.messageKey, limit)
}
