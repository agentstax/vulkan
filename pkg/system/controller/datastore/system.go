package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RegisterSystem creates the shared control-plane schema and resolves the
// singleton system row, returning it.
func (d *SystemDatastore) RegisterSystem(ctx context.Context) (*SystemData, error) {
	var registered *SystemData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		registered, err = d.registerSystem(ctx)
		return err
	})
	return registered, err
}

func (d *SystemDatastore) registerSystem(ctx context.Context) (*SystemData, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// txn-scoped -- acquired here, auto-released at commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1);`, common.AdvisoryLock); err != nil {
		return nil, err
	}

	if err := d.createSystemTables(ctx, tx); err != nil {
		return nil, err
	}
	registered, err := d.seedSystem(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := d.recordBaseline(ctx, tx, registered.Id); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "system schema registered")
	return registered, nil
}

// seedSystem seeds the singleton row, first register wins.
func (d *SystemDatastore) seedSystem(ctx context.Context, tx pgx.Tx) (*SystemData, error) {
	seedSystemSql := `
		INSERT INTO system (created_at, updated_at)
		SELECT NOW(), NOW()
		WHERE NOT EXISTS (SELECT 1 FROM system)
		RETURNING id, created_at, updated_at;
	`
	seeded, err := d.scanSystemData(tx.QueryRow(ctx, seedSystemSql))
	if err != nil {
		return nil, err
	}
	if seeded != nil {
		return seeded, nil
	}

	existing, err := d.getSystem(ctx, tx)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("system row missing right after seed -- unexpected")
	}
	return existing, nil
}

// recordBaseline records the baseline in migration_log, but only if there's no
// success row yet.
func (d *SystemDatastore) recordBaseline(ctx context.Context, tx pgx.Tx, systemId int64) error {
	recordBaselineSql := `
		INSERT INTO migration_log (system_id, migration_version, status)
		SELECT $1, 1, 'success'
		WHERE NOT EXISTS (
			SELECT 1 FROM migration_log
			WHERE system_id = $1 AND status = 'success'
		);
	`
	_, err := tx.Exec(ctx, recordBaselineSql, systemId)
	return err
}

// GetSystem returns the singleton system row, or (nil, nil) if the system
// hasn't been registered.
func (d *SystemDatastore) GetSystem(ctx context.Context) (*SystemData, error) {
	var systemData *SystemData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		systemData, err = d.getSystem(ctx, d.Datastore.Pool)
		return err
	})
	return systemData, err
}

func (d *SystemDatastore) getSystem(ctx context.Context, q datastore.Querier) (*SystemData, error) {
	sql := `
		SELECT id, created_at, updated_at
		FROM system;
	`
	return d.scanSystemData(q.QueryRow(ctx, sql))
}

// scanSystemData returns (nil, nil) when the row -- or the table itself,
// 42P01 -- isn't there yet.
func (d *SystemDatastore) scanSystemData(row pgx.Row) (*SystemData, error) {
	var data SystemData
	err := row.Scan(&data.Id, &data.CreatedAt, &data.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		// 42P01 = table does not exist
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil, nil
		}
		return nil, err
	}
	return &data, nil
}
