package vulkan

import (
	"context"
)

// GroupHandle is a consumer group named on its topic, holding no row.
// Get is the comma-ok read; every other verb returns the not-found error
// itself.
type GroupHandle[Message Versioned] struct {
	topicName string
	name      string
	client    *Client
}

// Groups returns every consumer group registered on the topic, ordered by
// name.
func (t *TopicHandle[Message]) Groups(ctx context.Context) ([]*Group, error) {
	return t.client.admin.ListGroups(ctx, t.name)
}

// Group names a consumer group on this topic. No I/O and no failure --
// each verb on the handle resolves both names when called.
func (t *TopicHandle[Message]) Group(name string) *GroupHandle[Message] {
	return &GroupHandle[Message]{topicName: t.name, name: name, client: t.client}
}

// Register resolves the topic and registers the consumer group on it,
// returning an instance that consumes the topic's Message. cfg is the
// group's declaration -- nil or sparse for the defaults, with cfg.Bindings
// the full pattern set (nil = the whole topic). A Logger or Retry left nil
// takes the client's.
func (g *GroupHandle[Message]) Register(ctx context.Context, cfg *ConsumerConfig) (*ConsumerInstance[Message], error) {
	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	if cfg.Logger == nil {
		cfg.Logger = g.client.Logger
	}
	if cfg.Retry == nil {
		cfg.Retry = g.client.Config.Retry
	}

	instance, err := g.client.consumer.Register[Message](ctx, g.name, g.topicName, cfg)
	if err != nil {
		return nil, err
	}
	return newConsumerInstance(instance, g.client.manager, !g.client.Config.DisableManager)
}

// Get reads the group's row. Returns (nil, nil) when the topic or the
// group is not registered.
func (g *GroupHandle[Message]) Get(ctx context.Context) (*Group, error) {
	return g.client.admin.GetGroup(ctx, g.topicName, g.name)
}

// Workers returns the group's worker rows -- its stored config.
func (g *GroupHandle[Message]) Workers(ctx context.Context) ([]*Worker, error) {
	return g.client.admin.ListGroupWorkers(ctx, g.topicName, g.name)
}

// Destroy permanently deletes the group: its cursor, bindings, leases,
// delivery rows, group-owned workers and schedules. The topic and its
// messages are untouched. Refused unless ClientConfig.AllowDestroy is set.
func (g *GroupHandle[Message]) Destroy(ctx context.Context, options *DestroyOptions) error {
	return g.client.admin.DestroyGroup(ctx, g.topicName, g.name, options)
}
