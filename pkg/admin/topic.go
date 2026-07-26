package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
	topicMigrations "github.com/agentstax/vulkan/pkg/topic/migrations"
)

// GetTopic resolves topic (name, version). Returns (nil, nil),
// not an error, if that version isn't registered under name.
func (a *MessageAdmin) GetTopic(ctx context.Context, name string, version topic.SchemaVersion) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	foundTopic, err := a.topicDatastore.GetTopic(ctx, name, version)
	if err != nil {
		return nil, err
	}
	return foundTopic, nil
}

// ListTopics returns every registered topic version, ordered by name.
func (a *MessageAdmin) ListTopics(ctx context.Context) ([]*topic.Topic, error) {
	return a.topicDatastore.ListTopics(ctx)
}

// RegisterTopic is idempotent -- an existing (name, version) resolves to its
// topic instead of erroring.
//
// name is dot-namespaced by domain and entity: <domain>.<entity>[.<event>].
// Safe to rename later -- topics are addressed by id internally, not name.
// Ex: "orders.created", "billing.invoice.paid"
//
// version must be >= 1; registering a version that doesn't exist yet under
// name is a whole new physical topic, never a migration of an existing one.
//
// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func (a *MessageAdmin) RegisterTopic(ctx context.Context, name string, version topic.SchemaVersion, cfg *topic.Config) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}
	if version < 1 {
		return nil, fmt.Errorf("SchemaVersion must be >= 1, got %d", version)
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

	return a.topicDatastore.UpsertTopic(ctx, name, version, *cfg)
}

// AlterTopic applies cfg's non-nil fields to topic (name, version) and
// returns the updated topic. Returns ErrTopicNotFound if that
// (name, version) isn't registered.
//
// Two consequences to plan around:
//   - Running producers/consumers snapshot the topic at their Register, so an
//     alter takes effect on their NEXT restart, not live.
//   - RegisterTopic calls still passing the pre-alter config will fail with
//     ErrTopicConfigMismatch -- deliberate, so declarative register calls
//     can't silently drift from what an operator changed.
func (a *MessageAdmin) AlterTopic(ctx context.Context, name string, version topic.SchemaVersion, cfg *topic.AlterConfig) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	if cfg == nil {
		cfg = &topic.AlterConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	updated, err := a.topicDatastore.UpdateTopic(ctx, name, version, cfg)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, fmt.Errorf("%w: %s version %d", topic.ErrTopicNotFound, name, version)
	}
	return updated, nil
}

// MigrateTopic moves topic (name, version) to targetVersion.
// Returns ErrTopicNotFound if that version isn't registered under name.
func (a *MessageAdmin) MigrateTopic(ctx context.Context, name string, version topic.SchemaVersion, targetVersion int64) error {
	found, err := a.GetTopic(ctx, name, version)
	if err != nil {
		return err
	}
	if found == nil {
		return fmt.Errorf("%w: %s version %d", topic.ErrTopicNotFound, name, version)
	}

	return a.migrateRunner.RunOnce(ctx, targetVersion, migrate.EntityTopic, found.Id, topicMigrations.Registry)
}

// MigrateTopics moves every registered topic's schema to targetVersion.
// A no-op, not an error, if no topics are registered.
func (a *MessageAdmin) MigrateTopics(ctx context.Context, targetVersion int64) error {
	return a.migrateRunner.RunAll(ctx, targetVersion, migrate.EntityTopic, topicMigrations.Registry)
}

// RenameTopic changes the name of every version registered under name.
// Returns ErrTopicNotFound if no version is registered under name.
// ErrTopicNameTaken if newName already has any topic / version registered.
//
// Running producers/consumers keep working (they resolved the id at their Register),
// but anything still CONFIGURED with the old name fails its next restart's Register.
func (a *MessageAdmin) RenameTopic(ctx context.Context, name string, newName string) ([]*topic.Topic, error) {
	if name == "" || newName == "" {
		return nil, errors.New("topic name and new name are required")
	}
	if newName == name {
		return nil, errors.New("new name matches the current name -- nothing to rename")
	}

	renamed, err := a.topicDatastore.RenameTopic(ctx, name, newName)
	if err != nil {
		return nil, err
	}
	if renamed == nil {
		return nil, fmt.Errorf("%w: %s", topic.ErrTopicNotFound, name)
	}
	return renamed, nil
}

// DestroyOptions configures a single DestroyTopic call.
type DestroyOptions struct {
	// Force - required to destroy a topic that still holds messages.
	// Default: false.
	Force bool
}

// DestroyTopic permanently drops topic (name, version) and every
// message it holds. Returns ErrDestroyDisabled unless
// MessageAdminConfig.AllowDestroy is set, ErrTopicNotFound if that version
// isn't registered under name, and ErrTopicNotEmpty if the topic still holds
// messages and opts.Force isn't set.
func (a *MessageAdmin) DestroyTopic(ctx context.Context, name string, version topic.SchemaVersion, opts DestroyOptions) error {
	if !a.allowDestroy {
		return ErrDestroyDisabled
	}
	if name == "" {
		return errors.New("topic name is required")
	}

	found, err := a.topicDatastore.GetTopic(ctx, name, version)
	if err != nil {
		return err
	}
	if found == nil {
		return fmt.Errorf("%w: %s version %d", topic.ErrTopicNotFound, name, version)
	}

	return a.destroyTopic(ctx, found, opts)
}

func (a *MessageAdmin) destroyTopic(ctx context.Context, found *topic.Topic, opts DestroyOptions) error {
	if !opts.Force {
		empty, err := a.topicDatastore.IsEmpty(ctx, found.Id)
		if err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("%w: %s", topic.ErrTopicNotEmpty, found.Name)
		}
	}

	return a.topicDatastore.DeleteTopic(ctx, found)
}
