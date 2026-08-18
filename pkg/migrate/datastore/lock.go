package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IsLocked reports whether any session currently holds the migration advisory
// lock -- a snapshot from pg_locks, not an acquisition. Another session can take
// the lock in the gap between this check and a subsequent AcquireLock.
func (d *MigrateDatastore) IsLocked(ctx context.Context) (bool, error) {
	var locked bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		locked, err = d.isLocked(ctx)
		return err
	})
	return locked, err
}

func (d *MigrateDatastore) isLocked(ctx context.Context) (bool, error) {
	var locked bool
	err := d.Datastore.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_locks
			WHERE locktype = 'advisory' AND classid = 0 AND objid = $1 AND granted
		);
	`, common.AdvisoryLock).Scan(&locked)
	return locked, err
}

// AcquireLock pins a connection and takes the SESSION-level lock -- session, not
// xact, so it outlives the per-step txns (an xact lock releases at each commit).
// Auto-released if the connection dies.
func (d *MigrateDatastore) AcquireLock(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := d.Datastore.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1);`, common.AdvisoryLock); err != nil {
		conn.Release()
		return nil, err
	}
	return conn, nil
}

func (d *MigrateDatastore) ReleaseLock(conn *pgxpool.Conn) {
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1);`, common.AdvisoryLock); err != nil {
		d.Logger.ErrorContext(context.Background(), "could not release migration advisory lock", "error", err.Error())
	}
	conn.Release()
}
