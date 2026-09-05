package vulkan

import (
	"context"
)

// TopicHandle is the topic's name plus the client, holding no row. Message
// is the payload type every handle under it reads and writes.
// Every verb resolves the name when called; Get is
// the comma-ok read, every other verb returns the not-found error itself.
type TopicHandle[Message Versioned] struct {
	name   string
	client *Client
}

// Topics returns every registered topic, ordered by name.
func (c *Client) Topics(ctx context.Context) ([]*Topic, error) {
	return c.admin.ListTopics(ctx)
}

// Topic names a topic on the client under the payload type Message. No I/O
// and no failure -- each verb on the handle resolves the name when called.
// A caller with no payload type in scope passes RawPayload.
func (c *Client) Topic[Message Versioned](name string) *TopicHandle[Message] {
	return &TopicHandle[Message]{name: name, client: c}
}

// Register declares the named topic, creating its tables on first
// registration. Idempotent; cfg may be nil or sparse.
func (t *TopicHandle[Message]) Register(ctx context.Context, cfg *TopicConfig) (*Topic, error) {
	return t.client.admin.RegisterTopic(ctx, t.name, cfg)
}

// Get reads the topic's row. Returns (nil, nil) when the topic is not
// registered.
func (t *TopicHandle[Message]) Get(ctx context.Context) (*Topic, error) {
	return t.client.admin.GetTopic(ctx, t.name)
}

func (t *TopicHandle[Message]) Migrate(ctx context.Context, targetVersion int64) error {
	return t.client.admin.MigrateTopic(ctx, t.name, targetVersion)
}

func (t *TopicHandle[Message]) Rename(ctx context.Context, newName string) (*Topic, error) {
	return t.client.admin.RenameTopic(ctx, t.name, newName)
}

// Destroy permanently deletes the topic, its messages, and every consumer
// group on it. Refused unless ClientConfig.AllowDestroy is set.
func (t *TopicHandle[Message]) Destroy(ctx context.Context, options *DestroyOptions) error {
	return t.client.admin.DestroyTopic(ctx, t.name, options)
}

func (t *TopicHandle[Message]) Health(ctx context.Context) ([]*TopicVersionHealth, error) {
	return t.client.admin.TopicHealth(ctx, t.name)
}

func (t *TopicHandle[Message]) Metrics(ctx context.Context) (*TopicSnapshot, error) {
	return t.client.admin.TopicMetrics(ctx, t.name)
}

// CompactionHeads returns every key's current compaction head on the
// topic, ordered by message key.
func (t *TopicHandle[Message]) CompactionHeads(ctx context.Context) ([]*StoredMessage[Message], error) {
	return t.client.admin.ListCompactionHeads[Message](ctx, t.name)
}
