package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// GetTopic resolves topic (name, version).
// Returns (nil, nil) if (name, version) is not found.
func (c *TopicController) GetTopic(ctx context.Context, name string, version topic.SchemaVersion) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}

	found, err := c.datastore.GetTopic(ctx, name, int64(version))
	if err != nil || found == nil {
		return nil, err
	}
	return toTopic(found)
}

// GetTopicById resolves a topic by its id. Returns (nil, nil) if no topic has it.
func (c *TopicController) GetTopicById(ctx context.Context, id int64) (*topic.Topic, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id must be > 0, got %d", id)
	}

	found, err := c.datastore.GetTopicById(ctx, id)
	if err != nil || found == nil {
		return nil, err
	}
	return toTopic(found)
}

func (c *TopicController) ListTopics(ctx context.Context) ([]*topic.Topic, error) {
	listed, err := c.datastore.ListTopics(ctx)
	if err != nil {
		return nil, err
	}

	var topics []*topic.Topic
	for _, data := range listed {
		listedTopic, err := toTopic(data)
		if err != nil {
			return nil, err
		}
		topics = append(topics, listedTopic)
	}
	return topics, nil
}

// RegisterTopic resolves (name, version) to its db identity, creating the
// topic if it doesn't exist, and returns the registered topic. cfg may be nil
// or a sparse struct -- WithDefaults fills every field left unset, Validate
// rejects what's out of range.
func (c *TopicController) RegisterTopic(ctx context.Context, systemId int64, name string, version topic.SchemaVersion, cfg *TopicConfig) (*topic.Topic, error) {
	if systemId <= 0 {
		return nil, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	if !topic.SlugPattern.MatchString(name) {
		return nil, fmt.Errorf("name must match %s, got %q", topic.SlugPattern, name)
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}
	if cfg == nil {
		cfg = &TopicConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	registered, err := c.datastore.RegisterTopic(ctx, toRegisterTopicData(systemId, name, version, cfg))
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

// UpdateTopic applies cfg's non-nil fields to topic's (name, version).
// Returns (nil, nil) if that (name, version) is not found.
func (c *TopicController) UpdateTopic(ctx context.Context, name string, version topic.SchemaVersion, cfg *AlterTopicConfig) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
	}
	if cfg == nil {
		cfg = &AlterTopicConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	updated, err := c.datastore.UpdateTopic(ctx, name, int64(version), toAlterTopicData(cfg))
	if err != nil || updated == nil {
		return nil, err
	}
	return toTopic(updated)
}

// RenameTopic moves every version under oldName to newName in one statement.
// Returns (nil, nil) if no version is registered under oldName
// ErrTopicNameTaken if newName already has any (name, version) registered.
func (c *TopicController) RenameTopic(ctx context.Context, oldName string, newName string) ([]*topic.Topic, error) {
	if oldName == "" {
		return nil, errors.New("oldName is required")
	}
	if !topic.SlugPattern.MatchString(newName) {
		return nil, fmt.Errorf("new name must match %s, got %q", topic.SlugPattern, newName)
	}

	renamed, err := c.datastore.RenameTopic(ctx, oldName, newName)
	if err != nil || renamed == nil {
		return nil, err
	}

	var topics []*topic.Topic
	for _, data := range renamed {
		renamedTopic, err := toTopic(data)
		if err != nil {
			return nil, err
		}
		topics = append(topics, renamedTopic)
	}
	return topics, nil
}

// DeleteTopic drains and drops the topic's tables, then removes its rows.
// name is only for error and log text.
func (c *TopicController) DeleteTopic(ctx context.Context, topicId int64, name string) error {
	if topicId <= 0 {
		return fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if name == "" {
		return errors.New("name is required")
	}
	return c.datastore.DeleteTopic(ctx, topicId, name)
}

// IsEmpty reports whether the topic's log holds any row at all.
func (c *TopicController) IsEmpty(ctx context.Context, topicId int64) (bool, error) {
	if topicId <= 0 {
		return false, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	return c.datastore.IsEmpty(ctx, topicId)
}
