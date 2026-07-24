package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/migrate"
	systemMigrations "github.com/agentstax/vulkan/pkg/system/migrations"
)

// RegisterSystem stands up the shared control-plane schema every topic rides
// on. Call it once before registering any topic.
//
// Idempotent and config-free -- safe to call on every service start, a no-op
// once the schema is present.
func (a *MessageAdmin) RegisterSystem(ctx context.Context) error {
	return a.systemDatastore.RegisterSystem(ctx)
}

// MigrateSystem moves the system schema to targetVersion.
// Returns an error ErrNotRegistered if RegisterSystem hasn't run.
func (a *MessageAdmin) MigrateSystem(ctx context.Context, targetVersion int64) error {
	return a.migrateRunner.Run(ctx, targetVersion, migrate.EntitySystem, 0, systemMigrations.Registry)
}
