package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
