package admin

import (
	"context"
	"errors"
	"strings"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	topicMigrations "github.com/agentstax/vulkan/pkg/topic/migrations"
)

// GetTopic resolves a topic by name. Returns (nil, nil), not an error,
// if name isn't registered.
func (a *MessageAdmin) GetTopic(ctx context.Context, name string) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}

	foundTopic, err := a.topicController.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return foundTopic, nil
}

// ListTopics returns every registered topic, ordered by name.
func (a *MessageAdmin) ListTopics(ctx context.Context) ([]*topic.Topic, error) {
	return a.topicController.List(ctx)
}

// RegisterTopic creates the named topic if it doesn't exist and returns
// it. Safe to call on every startup: cfg is applied on every call, so changing
// a value and redeploying changes the topic -- and two services passing
// different cfg for one topic will overwrite each other. Against an empty
// database it first stands up the control-plane schema -- RegisterSystem
// with a nil cfg.
//   - name: must match ^[a-z0-9._-]+$; dot-namespaced by domain and entity
//     ("orders.created", "billing.invoice.paid"); safe to rename later --
//     topics are addressed by id internally, not name
//   - cfg: may be nil or sparse -- WithDefaults fills every field left unset
//
// PartitionSize is fixed at creation; passing a different one returns
// ErrTopicConfigMismatch.
func (a *MessageAdmin) RegisterTopic(ctx context.Context, name string, cfg *topiccontroller.TopicConfig) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("topic name is required")
	}
	if isReservedTopicName(name) {
		return nil, topic.ErrReservedTopicName.With("topic", name)
	}

	// no system row means an empty database -- the first topic stands up the
	// control-plane schema with defaults; a customized system keeps its
	// declaration, since nothing runs when the row exists
	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		if err := a.RegisterSystem(ctx, nil); err != nil {
			return nil, err
		}
	}

	return a.registerTopic(ctx, name, cfg)
}

func (a *MessageAdmin) registerTopic(ctx context.Context, name string, cfg *topiccontroller.TopicConfig) (*topic.Topic, error) {
	// gate -- a topic can't exist without the control-plane schema it rides on;
	// otherwise RegisterTopic dies with a raw undefined-table error.
	sys, err := a.systemController.Get(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered.With("topic", name)
	}

	return a.topicController.Register(ctx, sys.Id, name, cfg)
}

// MigrateTopic moves the named topic's tables to targetVersion.
// Returns ErrTopicNotFound if name isn't registered.
func (a *MessageAdmin) MigrateTopic(ctx context.Context, name string, targetVersion int64) error {
	found, err := a.GetTopic(ctx, name)
	if err != nil {
		return err
	}
	if found == nil {
		return topic.ErrTopicNotFound.With("topic", name)
	}

	owner, err := common.NewTopicOwner(found.SystemId, found.Id, found.Name)
	if err != nil {
		return err
	}
	return a.migrateController.RunOnce(ctx, targetVersion, owner, topicMigrations.Registry)
}

// MigrateTopics moves every registered topic's schema to targetVersion.
// A no-op, not an error, if no topics are registered.
func (a *MessageAdmin) MigrateTopics(ctx context.Context, targetVersion int64) error {
	return a.migrateController.RunAll(ctx, targetVersion, common.OwnerTopic, topicMigrations.Registry)
}

// RenameTopic changes the topic's name. Returns ErrTopicNotFound if name
// isn't registered, ErrTopicNameTaken if newName already is.
//
// Running producers/consumers keep working (they resolved the id at their Register),
// but anything still CONFIGURED with the old name fails its next restart's Register.
func (a *MessageAdmin) RenameTopic(ctx context.Context, name string, newName string) (*topic.Topic, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if newName == "" {
		return nil, errors.New("newName is required")
	}
	if newName == name {
		return nil, errors.New("new name matches the current name -- nothing to rename")
	}
	if isReservedTopicName(name) || isReservedTopicName(newName) {
		return nil, topic.ErrReservedTopicName.With("topic", name, "new_name", newName)
	}

	renamed, err := a.topicController.Rename(ctx, name, newName)
	if err != nil {
		return nil, err
	}
	if renamed == nil {
		return nil, topic.ErrTopicNotFound.With("topic", name)
	}
	return renamed, nil
}

// DestroyOptions configures a single DestroyTopic call.
type DestroyOptions struct {
	// Force - required to destroy a topic that still holds messages.
	// Default: false.
	Force bool
}

// DestroyTopic permanently drops the named topic and every message it
// holds. Returns topic.ErrDestroyDisabled unless
// MessageAdminConfig.AllowDestroy is set, ErrTopicNotFound if name isn't
// registered, and ErrTopicNotEmpty if the topic still holds
// messages and options.Force isn't set.
func (a *MessageAdmin) DestroyTopic(ctx context.Context, name string, options DestroyOptions) error {
	if !a.allowDestroy {
		return topic.ErrDestroyDisabled
	}
	if name == "" {
		return errors.New("topic name is required")
	}
	if isReservedTopicName(name) {
		return topic.ErrReservedTopicName.With("topic", name)
	}

	found, err := a.topicController.Get(ctx, name)
	if err != nil {
		return err
	}
	if found == nil {
		return topic.ErrTopicNotFound.With("topic", name)
	}

	if !options.Force {
		if err := a.assertTopicIdle(ctx, found.Id, found.Name); err != nil {
			return err
		}
	}

	return a.topicController.Delete(ctx, found.Id, found.Name)
}

// assertTopicIdle is DestroyTopic's guard: no message would be discarded.
func (a *MessageAdmin) assertTopicIdle(ctx context.Context, topicId int64, name string) error {
	empty, err := a.topicController.IsEmpty(ctx, topicId)
	if err != nil {
		return err
	}
	if !empty {
		return topic.ErrTopicNotEmpty.With("topic", name, "topic_id", topicId)
	}
	return nil
}

// ***************
// *** HELPERS ***
// ***************

func isReservedTopicName(name string) bool {
	return strings.HasPrefix(name, common.SystemTopicPrefix)
}
