package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SystemData models the system table row exactly.
type SystemData struct {
	Id                    int64
	AlertRepeatIntervalNs int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// AlterSystemData is UpdateSystem's sparse patch -- a nil field means leave
// the column unchanged.
type AlterSystemData struct {
	AlertRepeatIntervalNs *int64
}

// RegisterSystem creates the shared control-plane schema and resolves the
// singleton system row, returning it. A row that already exists must match
// data or it errors with system.ErrSystemConfigMismatch.
func (d *SystemDatastore) RegisterSystem(ctx context.Context, data *SystemData) (*SystemData, error) {
	var registered *SystemData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		registered, err = d.registerSystem(ctx, data)
		return err
	})
	return registered, err
}

func (d *SystemDatastore) registerSystem(ctx context.Context, data *SystemData) (*SystemData, error) {
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
	registered, err := d.seedSystem(ctx, tx, data)
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

// seedSystem seeds the config row, first register wins. A row already there
// must match data -- the register is otherwise a silent no-op against a
// config the caller didn't ask for.
func (d *SystemDatastore) seedSystem(ctx context.Context, tx pgx.Tx, data *SystemData) (*SystemData, error) {
	seedSystemSql := `
		INSERT INTO system (alert_repeat_interval_ns)
		SELECT $1
		WHERE NOT EXISTS (SELECT 1 FROM system)
		RETURNING id, alert_repeat_interval_ns, created_at, updated_at;
	`
	seeded, err := d.scanSystemData(tx.QueryRow(ctx, seedSystemSql, data.AlertRepeatIntervalNs))
	if err != nil {
		return nil, err
	}
	if seeded != nil {
		// won the seed -- the row now holds exactly data
		return seeded, nil
	}

	existing, err := d.getSystem(ctx, tx)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("system config row missing right after seed -- unexpected")
	}
	if err := d.assertConfigMatches(existing, data); err != nil {
		return nil, err
	}
	return existing, nil
}

func (d *SystemDatastore) assertConfigMatches(found *SystemData, data *SystemData) error {
	if found.AlertRepeatIntervalNs != data.AlertRepeatIntervalNs {
		return fmt.Errorf("%w: existing=%+v got=%+v", system.ErrSystemConfigMismatch, *found, *data)
	}
	return nil
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
		SELECT id, alert_repeat_interval_ns, created_at, updated_at
		FROM system;
	`
	return d.scanSystemData(q.QueryRow(ctx, sql))
}

// UpdateSystem applies data's non-nil fields to the singleton system row and
// returns the updated row. Returns (nil, nil) if the row isn't there.
func (d *SystemDatastore) UpdateSystem(ctx context.Context, data *AlterSystemData) (*SystemData, error) {
	var systemData *SystemData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		systemData, err = d.updateSystem(ctx, data)
		return err
	})
	return systemData, err
}

func (d *SystemDatastore) updateSystem(ctx context.Context, data *AlterSystemData) (*SystemData, error) {
	// read-before-write is only for the old -> new log line
	old, err := d.getSystem(ctx, d.Datastore.Pool)
	if err != nil || old == nil {
		return nil, err
	}

	// a nil param reaches Postgres as NULL; COALESCE keeps the current value.
	sql := `
		UPDATE system
		SET
			alert_repeat_interval_ns = COALESCE($2, alert_repeat_interval_ns),
			updated_at = NOW()
		WHERE id = $1
		RETURNING id, alert_repeat_interval_ns, created_at, updated_at;
	`
	updated, err := d.scanSystemData(d.Datastore.Pool.QueryRow(ctx, sql, old.Id, data.AlertRepeatIntervalNs))
	if err != nil || updated == nil {
		// nil = destroyed between the read and the update
		return nil, err
	}

	d.Logger.InfoContext(ctx, "system config altered", alterLogFields(old, updated)...)
	return updated, nil
}

// scanSystemData returns (nil, nil) when the row -- or the table itself,
// 42P01 -- isn't there yet.
func (d *SystemDatastore) scanSystemData(row pgx.Row) (*SystemData, error) {
	var data SystemData
	err := row.Scan(&data.Id, &data.AlertRepeatIntervalNs, &data.CreatedAt, &data.UpdatedAt)
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

// alterLogFields renders old -> new pairs for just the columns that changed.
func alterLogFields(old, updated *SystemData) []any {
	fields := []any{}
	if old.AlertRepeatIntervalNs != updated.AlertRepeatIntervalNs {
		fields = append(fields, "alert_repeat_interval",
			fmt.Sprintf("%v -> %v", time.Duration(old.AlertRepeatIntervalNs), time.Duration(updated.AlertRepeatIntervalNs)))
	}
	return fields
}
