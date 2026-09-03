package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/consume"
)

// GetGroup resolves a consumer group by its owning topic and name.
// Returns (nil, nil) if the group is not registered on that topic.
func (c *ConsumeController) GetGroup(ctx context.Context, topicId int64, name string) (*consume.GroupData, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	data, err := c.datastore.GetGroup(ctx, topicId, name)
	if err != nil || data == nil {
		return nil, err
	}
	return toGroupData(data), nil
}

// ListGroups lists the topic's consumer groups, ordered by name.
func (c *ConsumeController) ListGroups(ctx context.Context, topicId int64) ([]*consume.GroupData, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}

	data, err := c.datastore.ListGroups(ctx, topicId)
	if err != nil {
		return nil, err
	}

	groups := make([]*consume.GroupData, 0, len(data))
	for _, row := range data {
		groups = append(groups, toGroupData(&row))
	}
	return groups, nil
}

// RegisterGroup creates the group and its cursor at start; an existing group
// is returned untouched, its position kept.
func (c *ConsumeController) RegisterGroup(ctx context.Context, topicId int64, name string, start consume.CursorPosition) (*consume.GroupData, error) {
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	if err := start.Kind.Validate(); err != nil {
		return nil, fmt.Errorf("start.Kind: %w", err)
	}

	data, err := c.datastore.RegisterGroup(ctx, topicId, name, start)
	if err != nil {
		return nil, err
	}
	return toGroupData(data), nil
}

// DeleteGroup deletes the group and every row it owns in one transaction.
// A running consumer stops itself: its worker rows vanish with the group,
// so its next heartbeat fails.
func (c *ConsumeController) DeleteGroup(ctx context.Context, topicId int64, groupId int64, name string) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if groupId <= 0 {
		return fmt.Errorf("groupId must be > 0, got %d", groupId)
	}
	if name == "" {
		return errors.New("name is required")
	}

	return c.datastore.DeleteGroup(ctx, topicId, groupId, name)
}
