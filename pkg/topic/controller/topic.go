package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// Get resolves a topic by name. Returns (nil, nil) if name is not found.
func (c *TopicController) Get(ctx context.Context, name string) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}

	found, err := c.datastore.Get(ctx, name)
	if err != nil || found == nil {
		return nil, err
	}
	return toTopic(found)
}

// GetById resolves a topic by its id. Returns (nil, nil) if no topic has it.
func (c *TopicController) GetById(ctx context.Context, id int64) (*topic.Topic, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id must be > 0, got %d", id)
	}

	found, err := c.datastore.GetById(ctx, id)
	if err != nil || found == nil {
		return nil, err
	}
	return toTopic(found)
}

func (c *TopicController) List(ctx context.Context) ([]*topic.Topic, error) {
	listed, err := c.datastore.List(ctx)
	if err != nil {
		return nil, err
	}

	var topics []*topic.Topic
	for _, data := range listed {
		listedTopic, err := toTopic(&data)
		if err != nil {
			return nil, err
		}
		topics = append(topics, listedTopic)
	}
	return topics, nil
}

// Register resolves name to its db identity, creating the
// topic if it doesn't exist, and returns the registered topic. cfg may be nil
// or a sparse struct -- WithDefaults fills every field left unset, Validate
// rejects what's out of range.
func (c *TopicController) Register(ctx context.Context, systemId int64, name string, cfg *TopicConfig) (*topic.Topic, error) {
	if systemId <= 0 {
		return nil, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	if !topic.SlugPattern.MatchString(name) {
		return nil, fmt.Errorf("name must match %s, got %q", topic.SlugPattern, name)
	}
	if cfg == nil {
		cfg = &TopicConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	registered, err := c.datastore.Register(ctx, toTopicData(systemId, name, cfg), common.ProcessIdentity)
	if err != nil {
		return nil, err
	}

	owner, err := common.NewTopicOwner(registered.SystemId, registered.Id, registered.Name)
	if err != nil {
		return nil, err
	}
	for _, declarer := range c.declarers {
		if err := declarer.Declare(ctx, owner); err != nil {
			return nil, err
		}
	}
	return toTopic(registered)
}

// Rename moves the topic under oldName to newName.
// Returns (nil, nil) if no topic is registered under oldName
// ErrTopicNameTaken if newName is already registered.
func (c *TopicController) Rename(ctx context.Context, oldName string, newName string) (*topic.Topic, error) {
	if oldName == "" {
		return nil, errors.New("oldName is required")
	}
	if !topic.SlugPattern.MatchString(newName) {
		return nil, fmt.Errorf("new name must match %s, got %q", topic.SlugPattern, newName)
	}

	renamed, err := c.datastore.Rename(ctx, oldName, newName, common.ProcessIdentity)
	if err != nil || renamed == nil {
		return nil, err
	}
	return toTopic(renamed)
}

// Delete drains and drops the topic's tables, then removes its rows.
// name is only for error and log text.
func (c *TopicController) Delete(ctx context.Context, topicId int64, name string) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if name == "" {
		return errors.New("name is required")
	}

	return c.datastore.Delete(ctx, topicId, name)
}

// IsEmpty reports whether the topic's log holds any row at all.
func (c *TopicController) IsEmpty(ctx context.Context, topicId int64) (bool, error) {
	if topicId <= 0 {
		return false, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	return c.datastore.IsEmpty(ctx, topicId)
}
