package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
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

	// Make sure the system's owned topics are registered:
	// - __system.metrics
	// - __system.alerts
	if err := a.ensureSystemTopic(ctx, metrics.TopicName, metrics.TopicConfig()); err != nil {
		return err
	}
	if err := a.ensureSystemTopic(ctx, alert.TopicName, alert.TopicConfig()); err != nil {
		return err
	}
	return nil
}

func (a *MessageAdmin) ensureSystemTopic(ctx context.Context, name string, cfg *topic.Config) error {
	existing, err := a.topicDatastore.GetTopic(ctx, name, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	// registerTopic bypasses the __system. reserved-name guard that RegisterTopic enforces.
	_, err = a.registerTopic(ctx, name, topic.SchemaVersion(1), cfg)
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
// returns the updated config. Returns migrate.ErrNotRegistered if
// RegisterSystem hasn't run.
//
// Two consequences:
//   - A consumer of this config snapshots it at startup, so an alter takes
//     effect on its NEXT restart, not live.
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
	sys, err := a.systemDatastore.GetConfig(ctx)
	if err != nil {
		return err
	}
	if sys == nil {
		return migrate.ErrNotRegistered
	}
	owner, err := common.NewSystemOwner(sys.Id)
	if err != nil {
		return err
	}
	return a.migrateRunner.RunOnce(ctx, targetVersion, owner, systemMigrations.Registry)
}
