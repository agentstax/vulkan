package controller

import (
	"context"
	"errors"
	"time"
)

// a consumer group is owned by exactly one topic -- names are unique per
// topic, not globally. Children (cursor, lease, binding) reference Id and
// carry no topic_id of their own; the topic_id FK cascade is the group's
// lifecycle -- destroying the topic destroys it.
type Group struct {
	Id        int64
	TopicId   int64
	Name      string
	CreatedAt time.Time
}

// GetGroup resolves a consumer group by its owning topic and name.
// Returns (nil, nil) if the group is not registered on that topic.
func (c *ConsumerController) GetGroup(ctx context.Context, topicId int64, name string) (*Group, error) {
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	data, err := c.datastore.GetGroup(ctx, topicId, name)
	if err != nil || data == nil {
		return nil, err
	}
	return toGroup(data), nil
}

// RegisterGroup creates the group and its cursor; an existing group is
// returned untouched.
func (c *ConsumerController) RegisterGroup(ctx context.Context, topicId int64, name string) (*Group, error) {
	if topicId <= 0 {
		return nil, errors.New("topicId must be > 0")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	data, err := c.datastore.RegisterGroup(ctx, topicId, name)
	if err != nil {
		return nil, err
	}
	return toGroup(data), nil
}

// DeleteGroup deletes the group and every row it owns in one transaction.
// A running consumer stops itself: its worker rows vanish with the group,
// so its next heartbeat fails.
func (c *ConsumerController) DeleteGroup(ctx context.Context, topicId int64, groupId int64, name string) error {
	if topicId <= 0 {
		return errors.New("topicId must be > 0")
	}
	if groupId <= 0 {
		return errors.New("groupId must be > 0")
	}
	if name == "" {
		return errors.New("name is required")
	}

	return c.datastore.DeleteGroup(ctx, topicId, groupId, name)
}
