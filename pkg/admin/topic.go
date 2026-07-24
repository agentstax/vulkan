package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
	topicMigrations "github.com/agentstax/vulkan/pkg/topic/migrations"
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

// ListTopics returns every registered topic, ordered by name.
func (a *MessageAdmin) ListTopics(ctx context.Context) ([]*topic.Topic, error) {
	return a.topicDatastore.ListTopics(ctx)
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

	// gate -- a topic can't exist without the control-plane schema it rides on;
	// otherwise UpsertTopic dies with a raw undefined-table error.
	registered, err := a.systemDatastore.IsRegistered(ctx)
	if err != nil {
		return nil, err
	}
	if !registered {
		return nil, fmt.Errorf("register the system with RegisterSystem before registering topic %q: %w", name, migrate.ErrNotRegistered)
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

// DestroyOptions configures a single DestroyTopic call.
type DestroyOptions struct {
	// Force - required to destroy a topic that still holds messages.
	// Default: false.
	Force bool
}

// DestroyTopic permanently drops the topic and every message it holds.
// Returns ErrDestroyDisabled unless MessageAdminConfig.AllowDestroy is set,
// ErrTopicNotFound if name isn't registered, and ErrTopicNotEmpty if the
// topic still holds messages and opts.Force isn't set.
func (a *MessageAdmin) DestroyTopic(ctx context.Context, name string, opts DestroyOptions) error {
	if !a.allowDestroy {
		return ErrDestroyDisabled
	}
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

	if !opts.Force {
		empty, err := a.topicDatastore.IsEmpty(ctx, found.Id)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("%w: %s", topic.ErrTopicNotEmpty, name)
		}
	}

	return a.topicDatastore.DeleteTopic(ctx, found)
}

// MigrateTopic moves a single topic's schema to targetVersion.
// Returns ErrTopicNotFound if name isn't registered.
func (a *MessageAdmin) MigrateTopic(ctx context.Context, name string, targetVersion int64) error {
	found, err := a.GetTopic(ctx, name)
	if err != nil {
		return err
	}
	if found == nil {
		return fmt.Errorf("%w: %s", topic.ErrTopicNotFound, name)
	}

	return a.migrateRunner.RunOnce(ctx, targetVersion, migrate.EntityTopic, found.Id, topicMigrations.Registry)
}

// MigrateTopics moves every registered topic's schema to targetVersion.
// A no-op, not an error, if no topics are registered.
func (a *MessageAdmin) MigrateTopics(ctx context.Context, targetVersion int64) error {
	return a.migrateRunner.RunAll(ctx, targetVersion, migrate.EntityTopic, topicMigrations.Registry)
}
