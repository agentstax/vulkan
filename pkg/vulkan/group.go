package vulkan

import (
	"context"
)

// Group is a handle: a consumer group named on its topic, holding no row.
// Get is the comma-ok read; every other verb returns the not-found error
// itself.
type Group struct {
	topicName string
	name      string
	client    *Client
}

// Get reads the group's row. Returns (nil, nil) when the topic or the
// group is not registered.
func (g *Group) Get(ctx context.Context) (*GroupData, error) {
	found, err := g.client.admin.GetTopic(ctx, g.topicName)
	if err != nil || found == nil {
		return nil, err
	}

	return g.client.groups.GetGroup(ctx, found.Id, g.name)
}

// ListWorkers lists the group's worker rows -- its stored config.
func (g *Group) ListWorkers(ctx context.Context) ([]*WorkerData, error) {
	return g.client.admin.GetGroup(ctx, g.topicName, g.name)
}

// Destroy permanently deletes the group: its cursor, bindings, leases,
// delivery rows, group-owned workers and schedules. The topic and its
// messages are untouched. Refused unless ClientConfig.AllowDestroy is set.
func (g *Group) Destroy(ctx context.Context, options *DestroyOptions) error {
	return g.client.admin.DestroyGroup(ctx, g.topicName, g.name, options)
}
