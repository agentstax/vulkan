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
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1);`, common.AdvisoryLock); err != nil {
		return err
	}

	// IMPORTANT - must be reverse order of createSystemTables' creation order
	// so every FK's referencing table is gone before its target drops.
	for _, table := range []string{
		"migration_log",
		"cron_job",
		"compaction_head",
		"binding_declaration",
		"binding",
		"worker_instance",
		"worker",
		"key_lease",
		"lease",
		"cursor",
		"consumer_group",
		"topic",
		"system",
	} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, table)); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, "system destroyed -- control-plane schema dropped")
	return nil
}
