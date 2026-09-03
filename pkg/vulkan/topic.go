package vulkan

import (
	"context"
)

// Topic is a handle: the topic's name plus the client, holding no row.
// Every verb resolves the name when called; Get is the comma-ok read,
// every other verb returns the not-found error itself.
type Topic struct {
	name   string
	client *Client
}

// Topic names a topic on the client. No I/O and no failure -- each verb
// on the handle resolves the name when called.
func (c *Client) Topic(name string) *Topic {
	return &Topic{name: name, client: c}
}

// Get reads the topic's row. Returns (nil, nil) when the topic is not
// registered.
func (t *Topic) Get(ctx context.Context) (*TopicData, error) {
	return t.client.admin.GetTopic(ctx, t.name)
}

func (t *Topic) Migrate(ctx context.Context, targetVersion int64) error {
	return t.client.admin.MigrateTopic(ctx, t.name, targetVersion)
}

func (t *Topic) Rename(ctx context.Context, newName string) (*TopicData, error) {
	return t.client.admin.RenameTopic(ctx, t.name, newName)
}

// Destroy permanently deletes the topic, its messages, and every consumer
// group on it. Refused unless ClientConfig.AllowDestroy is set.
func (t *Topic) Destroy(ctx context.Context, options *DestroyOptions) error {
	return t.client.admin.DestroyTopic(ctx, t.name, options)
}

func (t *Topic) Health(ctx context.Context) ([]*VersionHealth, error) {
	return t.client.admin.TopicHealth(ctx, t.name)
}

func (t *Topic) Metrics(ctx context.Context) (*TopicSnapshot, error) {
	return t.client.admin.TopicMetrics(ctx, t.name)
}

// CompactionHead returns messageKey's current compaction head, or nil if
// nothing has been produced under it.
func (t *Topic) CompactionHead[Message Versioned](ctx context.Context, messageKey string) (*MessageData[Message], error) {
	return t.client.admin.GetCompactionHead[Message](ctx, t.name, messageKey)
}

// ListKeyMessages returns messageKey's retained messages, newest first.
func (t *Topic) ListKeyMessages[Message Versioned](ctx context.Context, messageKey string, limit int) ([]*MessageData[Message], error) {
	return t.client.admin.ListKeyMessages[Message](ctx, t.name, messageKey, limit)
}

// ListGroups lists the topic's consumer groups, ordered by name.
func (t *Topic) ListGroups(ctx context.Context) ([]*GroupData, error) {
	return t.client.admin.ListGroups(ctx, t.name)
}

// Group names a consumer group on this topic. No I/O and no failure --
// each verb on the handle resolves both names when called.
func (t *Topic) Group(name string) *Group {
	return &Group{topicName: t.name, name: name, client: t.client}
}
