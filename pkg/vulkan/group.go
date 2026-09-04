package vulkan

import (
	"context"
)

// GroupHandle is a consumer group named on its topic, holding no row.
// Get is the comma-ok read; every other verb returns the not-found error
// itself.
type GroupHandle struct {
	topicName string
	name      string
	client    *Client
}

// Groups returns every consumer group registered on the topic, ordered by
// name.
func (t *TopicHandle) Groups(ctx context.Context) ([]*Group, error) {
	return t.client.admin.ListGroups(ctx, t.name)
}

// Group names a consumer group on this topic. No I/O and no failure --
// each verb on the handle resolves both names when called.
func (t *TopicHandle) Group(name string) *GroupHandle {
	return &GroupHandle{topicName: t.name, name: name, client: t.client}
}

// Get reads the group's row. Returns (nil, nil) when the topic or the
// group is not registered.
func (g *GroupHandle) Get(ctx context.Context) (*Group, error) {
	return g.client.admin.GetGroup(ctx, g.topicName, g.name)
}

// Workers returns the group's worker rows -- its stored config.
func (g *GroupHandle) Workers(ctx context.Context) ([]*Worker, error) {
	return g.client.admin.ListGroupWorkers(ctx, g.topicName, g.name)
}

// Destroy permanently deletes the group: its cursor, bindings, leases,
// delivery rows, group-owned workers and schedules. The topic and its
// messages are untouched. Refused unless ClientConfig.AllowDestroy is set.
func (g *GroupHandle) Destroy(ctx context.Context, options *DestroyOptions) error {
	return g.client.admin.DestroyGroup(ctx, g.topicName, g.name, options)
}
