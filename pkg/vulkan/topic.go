package vulkan

import (
	"context"
)

// TopicHandle is the topic's name plus the client, holding no row.
// Every verb resolves the name when called; Get is the comma-ok read,
// every other verb returns the not-found error itself.
type TopicHandle struct {
	name   string
	client *Client
}

// Topics returns every registered topic, ordered by name.
func (c *Client) Topics(ctx context.Context) ([]*Topic, error) {
	return c.admin.ListTopics(ctx)
}

// Topic names a topic on the client. No I/O and no failure -- each verb
// on the handle resolves the name when called.
func (c *Client) Topic(name string) *TopicHandle {
	return &TopicHandle{name: name, client: c}
}

// Register declares the named topic, creating its tables on first
// registration. Idempotent; cfg may be nil or sparse.
func (t *TopicHandle) Register(ctx context.Context, cfg *TopicConfig) (*Topic, error) {
	return t.client.admin.RegisterTopic(ctx, t.name, cfg)
}

// Get reads the topic's row. Returns (nil, nil) when the topic is not
// registered.
func (t *TopicHandle) Get(ctx context.Context) (*Topic, error) {
	return t.client.admin.GetTopic(ctx, t.name)
}

func (t *TopicHandle) Migrate(ctx context.Context, targetVersion int64) error {
	return t.client.admin.MigrateTopic(ctx, t.name, targetVersion)
}

func (t *TopicHandle) Rename(ctx context.Context, newName string) (*Topic, error) {
	return t.client.admin.RenameTopic(ctx, t.name, newName)
}

// Destroy permanently deletes the topic, its messages, and every consumer
// group on it. Refused unless ClientConfig.AllowDestroy is set.
func (t *TopicHandle) Destroy(ctx context.Context, options *DestroyOptions) error {
	return t.client.admin.DestroyTopic(ctx, t.name, options)
}

func (t *TopicHandle) Health(ctx context.Context) ([]*TopicVersionHealth, error) {
	return t.client.admin.TopicHealth(ctx, t.name)
}

func (t *TopicHandle) Metrics(ctx context.Context) (*TopicSnapshot, error) {
	return t.client.admin.TopicMetrics(ctx, t.name)
}

// CompactionHead returns messageKey's current compaction head, or
// ErrCompactionHeadNotFound if no compacted message was produced under it.
func (t *TopicHandle) CompactionHead[Payload Versioned](ctx context.Context, messageKey string) (*Message[Payload], error) {
	return t.client.admin.GetCompactionHead[Payload](ctx, t.name, messageKey)
}

// KeyMessages returns messageKey's retained messages, newest first.
func (t *TopicHandle) KeyMessages[Payload Versioned](ctx context.Context, messageKey string, limit int) ([]*Message[Payload], error) {
	return t.client.admin.ListKeyMessages[Payload](ctx, t.name, messageKey, limit)
}
