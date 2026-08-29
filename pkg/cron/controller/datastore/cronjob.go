package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

// Get returns (nil, nil) if not found.
func (d *CronJobDatastore) Get(ctx context.Context, name string) (*CronJobData, error) {
	var job *CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		job, err = d.get(ctx, d.Datastore.Pool, name)
		return err
	})
	return job, err
}

func (d *CronJobDatastore) get(ctx context.Context, q datastore.Querier, name string) (*CronJobData, error) {
	sql := `
		-- vulkan: cron.get
		SELECT
			cron_job_config.id,
			COALESCE(cron_job_config.system_id, 0),
			COALESCE(cron_job_config.topic_id, 0),
			COALESCE(cron_job_config.consumer_group_id, 0),
			cron_job_config.name,
			cron_job_config.schedule,
			cron_job_config.concurrency,
			cron_job_config.timeout_ns,
			cron_job_config.suspended,
			cron_job_config.payload,
			cron_job_config.metadata,
			cron_job_cursor.next_scheduled_at,
			cron_job_cursor.last_scheduled_at
		FROM cron_job_config
		JOIN cron_job_cursor ON cron_job_cursor.cron_job_id = cron_job_config.id
		WHERE cron_job_config.name = $1;
	`
	return d.scanCronJobData(q.QueryRow(ctx, sql, name))
}

func (d *CronJobDatastore) List(ctx context.Context) ([]CronJobData, error) {
	var jobs []CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		jobs, err = d.list(ctx)
		return err
	})
	return jobs, err
}

func (d *CronJobDatastore) list(ctx context.Context) ([]CronJobData, error) {
	sql := `
		-- vulkan: cron.list
		SELECT
			cron_job_config.id,
			COALESCE(cron_job_config.system_id, 0),
			COALESCE(cron_job_config.topic_id, 0),
			COALESCE(cron_job_config.consumer_group_id, 0),
			cron_job_config.name,
			cron_job_config.schedule,
			cron_job_config.concurrency,
			cron_job_config.timeout_ns,
			cron_job_config.suspended,
			cron_job_config.payload,
			cron_job_config.metadata,
			cron_job_cursor.next_scheduled_at,
			cron_job_cursor.last_scheduled_at
		FROM cron_job_config
		JOIN cron_job_cursor ON cron_job_cursor.cron_job_id = cron_job_config.id
		ORDER BY cron_job_config.name;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []CronJobData
	for rows.Next() {
		job, err := d.scanCronJobData(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (d *CronJobDatastore) Suspend(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.suspend(ctx, name)
	})
}

func (d *CronJobDatastore) suspend(ctx context.Context, name string) error {
	tag, err := d.Datastore.Pool.Exec(ctx, `
		-- vulkan: cron.suspend
		UPDATE cron_job_config SET suspended = true WHERE name = $1;
	`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return cron.ErrCronJobNotFound.With("cron_job", name)
	}
	d.Logger.InfoContext(ctx, "cron job suspended", "cron_job", name)
	return nil
}

// Unsuspend resumes at Next(now()) -- a scheduled time that came due while
// suspended is dropped, not produced late.
func (d *CronJobDatastore) Unsuspend(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.unsuspend(ctx, name)
	})
}

func (d *CronJobDatastore) unsuspend(ctx context.Context, name string) error {
	job, err := d.get(ctx, d.Datastore.Pool, name)
	if err != nil {
		return err
	}
	if job == nil {
		return cron.ErrCronJobNotFound.With("cron_job", name)
	}

	schedule, err := cron.ParseSchedule(job.Schedule)
	if err != nil {
		return fmt.Errorf("schedule %q: %w", job.Schedule, err)
	}
	next, err := d.nextScheduledTime(ctx, d.Datastore.Pool, schedule)
	if err != nil {
		return fmt.Errorf("cron job %q stays suspended: %w", name, err)
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		-- vulkan: cron.unsuspend
		UPDATE cron_job_config SET suspended = false WHERE name = $1;
	`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return cron.ErrCronJobNotFound.With("cron_job", name)
	}

	if _, err := tx.Exec(ctx, `
		-- vulkan: cron.unsuspend
		UPDATE cron_job_cursor SET next_scheduled_at = $2 WHERE cron_job_id = $1;
	`, job.Id, next); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.InfoContext(ctx, "cron job unsuspended", "cron_job", name, "next_scheduled_at", next)
	return nil
}

// with N replicas running in different timezones they all have different
// clocks, getting time from db normalizes to a single source.
func (d *CronJobDatastore) dbNow(ctx context.Context, q datastore.Querier) (time.Time, error) {
	var now time.Time
	err := q.QueryRow(ctx, `
		-- vulkan: cron.dbNow
		SELECT now();
	`).Scan(&now)
	return now, err
}

// nextScheduledTime is the first scheduled time schedule produces after the db
// clock. A schedule with none left (a Feb-29 rule past its last leap year)
// cannot be registered.
func (d *CronJobDatastore) nextScheduledTime(ctx context.Context, q datastore.Querier, schedule *cron.Schedule) (time.Time, error) {
	dbNow, err := d.dbNow(ctx, q)
	if err != nil {
		return time.Time{}, err
	}
	next := schedule.Next(dbNow)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("schedule %q has no scheduled time after %v", schedule, dbNow)
	}
	return next, nil
}

func (d *CronJobDatastore) Delete(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.delete(ctx, name)
	})
}

func (d *CronJobDatastore) delete(ctx context.Context, name string) error {
	tag, err := d.Datastore.Pool.Exec(ctx, `
		-- vulkan: cron.delete
		DELETE FROM cron_job_config WHERE name = $1;
	`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return cron.ErrCronJobNotFound.With("cron_job", name)
	}
	d.Logger.InfoContext(ctx, "cron job destroyed", "cron_job", name)
	return nil
}

// scanCronJobData scans a row shaped like getCronJob's SELECT -- the column
// list every one of those queries shares.
func (d *CronJobDatastore) scanCronJobData(row pgx.Row) (*CronJobData, error) {
	var data CronJobData
	err := row.Scan(
		&data.Id,
		&data.SystemId,
		&data.TopicId,
		&data.ConsumerGroupId,
		&data.Name,
		&data.Schedule,
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
