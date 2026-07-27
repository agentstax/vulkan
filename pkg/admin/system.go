package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/system"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RegisterSystem stands up the shared control-plane schema every topic uses.
// Call it once before registering any topic.
//
// cfg may be nil or a sparse struct.
//
// Idempotent -- a no-op once the schema is present and cfg matches the seeded
// row, so it's safe on every start. A later call whose cfg DIFFERS fails with
// system.ErrSystemConfigMismatch.
func (a *MessageAdmin) RegisterSystem(ctx context.Context, cfg *system.Config) error {
	if cfg == nil {
		cfg = &system.Config{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	if err := a.systemDatastore.RegisterSystem(ctx, *cfg); err != nil {
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

// GetSystem returns the singleton system config. Returns
// migrate.ErrNotRegistered if RegisterSystem hasn't run.
func (a *MessageAdmin) GetSystem(ctx context.Context) (*system.System, error) {
	sys, err := a.systemDatastore.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return nil, migrate.ErrNotRegistered
	}
	return sys, nil
}

// AlterSystem applies cfg's non-nil fields to the singleton system config and
// returns the updated config.
// Returns migrate.ErrNotRegistered if RegisterSystem hasn't run.
//
// Two consequences, same as AlterTopic:
//   - The advisor duty snapshots this at its Register, so an alter takes effect
//     on its NEXT restart, not live.
//   - A RegisterSystem call still passing the pre-alter cfg fails with
//     system.ErrSystemConfigMismatch.
func (a *MessageAdmin) AlterSystem(ctx context.Context, cfg *system.AlterConfig) (*system.System, error) {
	if cfg == nil {
		cfg = &system.AlterConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	updated, err := a.systemDatastore.UpdateConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, migrate.ErrNotRegistered
	}
	return updated, nil
}

// MigrateSystem moves the system schema to targetVersion.
// Returns an error ErrNotRegistered if RegisterSystem hasn't run.
func (a *MessageAdmin) MigrateSystem(ctx context.Context, targetVersion int64) error {
	// 0 = system entityId by convention
	return a.migrateRunner.RunOnce(ctx, targetVersion, migrate.EntitySystem, 0, systemMigrations.Registry)
}
