package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

func (d *SystemDatastore) Delete(ctx context.Context) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.delete(ctx)
	})
}

func (d *SystemDatastore) delete(ctx context.Context) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// txn-scoped, same lock Register takes -- a concurrent register
	// waits here and recreates the schema after the drop commits.
	if _, err := tx.Exec(ctx, `
		-- vulkan: system.delete
		SELECT pg_advisory_xact_lock($1);
	`, common.AdvisoryLock); err != nil {
		return err
	}

	// IMPORTANT - must be reverse order of createSystemTables' creation order
	// so every FK's referencing table is gone before its target drops.
	for _, table := range []string{
		"migration_log",
		"schedule_cursor",
		"schedule_config",
		"worker_instance",
		"worker_config_log",
		"worker_config",
		"consumer_group_config",
		"topic_config_log",
		"topic_config",
		"system_config",
	} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			-- vulkan: system.delete
			DROP TABLE IF EXISTS %s;
		`, table)); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.InfoContext(ctx, "system destroyed -- control-plane tables dropped")
	return nil
}
