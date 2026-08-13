package controller

import (
	"context"
	"errors"
)

// Binding is one group's routing rule with the names a listing shows.
type Binding struct {
	GroupName     string
	TopicName     string
	SchemaVersion int64
	Pattern       string
}

// Bind scopes a group's routing to events whose routing_key matches a wildcard
// pattern ('*' matches any run of characters, any depth -- e.g.
// "orders.*.created" also matches "orders.us.central1.created"). A group with
// no binding at all matches every event.
//
// Binding changes apply forward only: fan-out never revisits messages below
// the group's cursor, so history a previous binding skipped stays skipped.
func (c *ConsumerController) Bind(ctx context.Context, groupId int64, pattern string) error {
	if groupId <= 0 {
		return errors.New("groupId must be > 0")
	}
	if pattern == "" {
		return errors.New("pattern is required")
	}

	return c.datastore.Bind(ctx, groupId, pattern)
}

// ClearBindings removes every binding for a group -> it goes back to matching
// all events on its topic, forward only (see Bind).
func (c *ConsumerController) ClearBindings(ctx context.Context, groupId int64) error {
	if groupId <= 0 {
		return errors.New("groupId must be > 0")
	}

	return c.datastore.ClearBindings(ctx, groupId)
}

// ListBindings returns every binding across all groups and topics. Groups
// with no rows here match every event on their topic.
func (c *ConsumerController) ListBindings(ctx context.Context) ([]*Binding, error) {
	data, err := c.datastore.ListBindings(ctx)
	if err != nil {
		return nil, err
	}

	bindings := make([]*Binding, 0, len(data))
	for i := range data {
		bindings = append(bindings, toBinding(&data[i]))
	}
	return bindings, nil
}
