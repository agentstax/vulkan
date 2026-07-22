package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
)

// GetTopic looks up a topic by name. Returns (nil, nil), not an error, if no
// topic is registered under that name.
func (a *MessageAdmin) GetTopic(ctx context.Context, name string) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	foundTopic, err := a.topicDatastore.GetTopic(ctx, name)
	if err != nil {
		return nil, err
	}
	return foundTopic, nil
}

// RegisterTopic is idempotent -- an existing name resolves to its topic
// instead of erroring.
//
// name is dot-namespaced by domain and entity: <domain>.<entity>[.<event>].
// Safe to rename later -- topics are addressed by id internally, not name.
// Ex: "orders.created", "billing.invoice.paid"
//
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func (a *MessageAdmin) RegisterTopic(ctx context.Context, name string, cfg *topic.Config) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	if cfg == nil {
		cfg = &topic.Config{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return a.topicDatastore.UpsertTopic(ctx, name, *cfg)
}

// DestroyTopic permanently drops the topic and every message it holds.
// Returns ErrTopicNotFound if name isn't registered.
func (a *MessageAdmin) DestroyTopic(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("topic name is required")
	}

	found, err := a.topicDatastore.GetTopic(ctx, name)
	if err != nil {
		return err
	}
	if found == nil {
		return fmt.Errorf("%w: %s", topic.ErrTopicNotFound, name)
	}

	return a.topicDatastore.DeleteTopic(ctx, found)
}
