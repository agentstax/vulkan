package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/jackc/pgx/v5"
)

// Get returns (nil, nil) if not found.
func (d *ScheduleDatastore) Get(ctx context.Context, name string) (*ScheduleData, error) {
	var found *ScheduleData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		found, err = d.get(ctx, d.Datastore.Pool, name)
		return err
	})
	return found, err
}

func (d *ScheduleDatastore) get(ctx context.Context, q datastore.Querier, name string) (*ScheduleData, error) {
	sql := `
		-- vulkan: schedule.get
		SELECT
			schedule_config.id,
			schedule_config.system_id,
			schedule_config.topic_id,
			schedule_config.name,
			schedule_config.expression,
			schedule_config.schema_version,
			schedule_config.concurrency,
			schedule_config.timeout_ns,
			schedule_config.suspended,
			schedule_config.payload,
			schedule_config.metadata,
			schedule_cursor.next_scheduled_at,
			schedule_cursor.last_scheduled_at
		FROM schedule_config
		JOIN schedule_cursor ON schedule_cursor.schedule_id = schedule_config.id
		WHERE schedule_config.name = $1;
	`
	return d.scanScheduleData(q.QueryRow(ctx, sql, name))
}

func (d *ScheduleDatastore) List(ctx context.Context) ([]ScheduleData, error) {
	var schedules []ScheduleData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		schedules, err = d.list(ctx)
		return err
	})
	return schedules, err
}

func (d *ScheduleDatastore) list(ctx context.Context) ([]ScheduleData, error) {
	sql := `
		-- vulkan: schedule.list
		SELECT
			schedule_config.id,
			schedule_config.system_id,
			schedule_config.topic_id,
			schedule_config.name,
			schedule_config.expression,
			schedule_config.schema_version,
			schedule_config.concurrency,
			schedule_config.timeout_ns,
			schedule_config.suspended,
			schedule_config.payload,
			schedule_config.metadata,
			schedule_cursor.next_scheduled_at,
			schedule_cursor.last_scheduled_at
		FROM schedule_config
		JOIN schedule_cursor ON schedule_cursor.schedule_id = schedule_config.id
		ORDER BY schedule_config.name;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []ScheduleData
	for rows.Next() {
		found, err := d.scanScheduleData(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, *found)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return schedules, nil
}

func (d *ScheduleDatastore) Suspend(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.suspend(ctx, name)
	})
}

func (d *ScheduleDatastore) suspend(ctx context.Context, name string) error {
	tag, err := d.Datastore.Pool.Exec(ctx, `
		-- vulkan: schedule.suspend
		UPDATE schedule_config SET suspended = true WHERE name = $1;
	`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return schedule.ErrScheduleNotFound.With("schedule", name)
	}
	d.Logger.InfoContext(ctx, "schedule suspended", "schedule", name)
	return nil
}

// Unsuspend resumes at Next(now()) -- a scheduled time that came due while
// suspended is dropped, not produced late.
func (d *ScheduleDatastore) Unsuspend(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.unsuspend(ctx, name)
	})
}

func (d *ScheduleDatastore) unsuspend(ctx context.Context, name string) error {
	found, err := d.get(ctx, d.Datastore.Pool, name)
	if err != nil {
		return err
	}
	if found == nil {
		return schedule.ErrScheduleNotFound.With("schedule", name)
	}

	expression, err := schedule.ParseExpression(found.Expression)
	if err != nil {
		return fmt.Errorf("expression %q: %w", found.Expression, err)
	}
	next, err := d.nextScheduledTime(ctx, d.Datastore.Pool, expression)
	if err != nil {
		return fmt.Errorf("schedule %q stays suspended: %w", name, err)
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		-- vulkan: schedule.unsuspend
		UPDATE schedule_config SET suspended = false WHERE name = $1;
	`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return schedule.ErrScheduleNotFound.With("schedule", name)
	}

	if _, err := tx.Exec(ctx, `
		-- vulkan: schedule.unsuspend
		UPDATE schedule_cursor SET next_scheduled_at = $2 WHERE schedule_id = $1;
	`, found.Id, next); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.InfoContext(ctx, "schedule unsuspended", "schedule", name, "next_scheduled_at", next)
	return nil
}

// with N replicas running in different timezones they all have different
// clocks, getting time from db normalizes to a single source.
func (d *ScheduleDatastore) dbNow(ctx context.Context, q datastore.Querier) (time.Time, error) {
	var now time.Time
	err := q.QueryRow(ctx, `
		-- vulkan: schedule.dbNow
		SELECT now();
	`).Scan(&now)
	return now, err
}

// nextScheduledTime is the first scheduled time schedule produces after the db
// clock. A schedule with none left (a Feb-29 rule past its last leap year)
// cannot be registered.
func (d *ScheduleDatastore) nextScheduledTime(ctx context.Context, q datastore.Querier, expression *schedule.Expression) (time.Time, error) {
	dbNow, err := d.dbNow(ctx, q)
	if err != nil {
		return time.Time{}, err
	}
	next := expression.Next(dbNow)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("expression %q has no scheduled time after %v", expression, dbNow)
	}
	return next, nil
}

func (d *ScheduleDatastore) Delete(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.delete(ctx, name)
	})
}

func (d *ScheduleDatastore) delete(ctx context.Context, name string) error {
	tag, err := d.Datastore.Pool.Exec(ctx, `
		-- vulkan: schedule.delete
		DELETE FROM schedule_config WHERE name = $1;
	`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return schedule.ErrScheduleNotFound.With("schedule", name)
	}
	d.Logger.InfoContext(ctx, "schedule destroyed", "schedule", name)
	return nil
}

// scanScheduleData scans a row shaped like getSchedule's SELECT -- the column
// list every one of those queries shares.
func (d *ScheduleDatastore) scanScheduleData(row pgx.Row) (*ScheduleData, error) {
	var data ScheduleData
	err := row.Scan(
		&data.Id,
		&data.SystemId,
		&data.TopicId,
		&data.Name,
		&data.Expression,
		&data.SchemaVersion,
		&data.Concurrency,
		&data.TimeoutNs,
		&data.Suspended,
		&data.Payload,
		&data.Metadata,
		&data.NextScheduledAt,
		&data.LastScheduledAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &data, nil
}
