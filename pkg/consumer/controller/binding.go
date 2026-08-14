package controller

import (
	"context"
)

// Binding is one group's routing rule with the names a listing shows.
type Binding struct {
	GroupName     string
	TopicName     string
	SchemaVersion int64
	Pattern       string
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
