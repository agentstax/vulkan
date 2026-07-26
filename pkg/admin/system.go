package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/migrate"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RegisterSystem stands up the shared control-plane schema every topic rides
// on. Call it once before registering any topic.
//
// Idempotent and config-free -- safe to call on every service start, a no-op
// once the schema is present.
func (a *MessageAdmin) RegisterSystem(ctx context.Context) error {
	if err := a.systemDatastore.RegisterSystem(ctx); err != nil {
		return err
	}

	existing, err := a.topicDatastore.GetTopic(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	_, err = a.registerTopic(ctx, metrics.TopicName, topic.SchemaVersion(1), metrics.TopicConfig())
	return err
}

// MigrateSystem moves the system schema to targetVersion.
// Returns an error ErrNotRegistered if RegisterSystem hasn't run.
func (a *MessageAdmin) MigrateSystem(ctx context.Context, targetVersion int64) error {
	// 0 = system entityId by convention
	return a.migrateRunner.RunOnce(ctx, targetVersion, migrate.EntitySystem, 0, systemMigrations.Registry)
}
