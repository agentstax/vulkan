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

// GetCronJob returns (nil, nil) if not found.
func (d *CronJobDatastore) GetCronJob(ctx context.Context, name string) (*CronJobData, error) {
	var job *CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		job, err = d.getCronJob(ctx, d.Datastore.Pool, name)
		return err
	})
	return job, err
}

func (d *CronJobDatastore) getCronJob(ctx context.Context, q datastore.Querier, name string) (*CronJobData, error) {
	sql := `
		SELECT
			id,
			COALESCE(system_id, 0),
			COALESCE(topic_id, 0),
			COALESCE(consumer_group_id, 0),
			name,
			schedule,
			concurrency,
			timeout_ns,
			suspended,
			data,
			metadata,
			next_scheduled_time,
			last_scheduled_time
		FROM cron_job
		WHERE name = $1;
	`
	return d.scanCronJobData(q.QueryRow(ctx, sql, name))
}

func (d *CronJobDatastore) ListCronJobs(ctx context.Context) ([]*CronJobData, error) {
	var jobs []*CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		jobs, err = d.listCronJobs(ctx)
		return err
	})
	return jobs, err
}

func (d *CronJobDatastore) listCronJobs(ctx context.Context) ([]*CronJobData, error) {
	sql := `
		SELECT
			id,
			COALESCE(system_id, 0),
			COALESCE(topic_id, 0),
			COALESCE(consumer_group_id, 0),
			name,
			schedule,
			concurrency,
			timeout_ns,
			suspended,
			data,
			metadata,
			next_scheduled_time,
			last_scheduled_time
		FROM cron_job
		ORDER BY name;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*CronJobData
	for rows.Next() {
		job, err := d.scanCronJobData(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (d *CronJobDatastore) SuspendCronJob(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.suspendCronJob(ctx, name)
	})
}

func (d *CronJobDatastore) suspendCronJob(ctx context.Context, name string) error {
	tag, err := d.Datastore.Pool.Exec(ctx, `UPDATE cron_job SET suspended = true WHERE name = $1;`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}
	d.Logger.InfoContext(ctx, "cron job suspended", "cron_job", name)
	return nil
}

// UnsuspendCronJob resumes at Next(now()) -- a scheduled time that came due while
// suspended is dropped, not produced late.
func (d *CronJobDatastore) UnsuspendCronJob(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.unsuspendCronJob(ctx, name)
	})
}

func (d *CronJobDatastore) unsuspendCronJob(ctx context.Context, name string) error {
	job, err := d.getCronJob(ctx, d.Datastore.Pool, name)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}

	schedule, err := cron.ParseSchedule(job.Schedule)
	if err != nil {
		return fmt.Errorf("schedule %q: %w", job.Schedule, err)
	}
	dbNow, err := d.dbNow(ctx, d.Datastore.Pool)
	if err != nil {
		return err
	}
	next := schedule.Next(dbNow)
	if next.IsZero() {
		return fmt.Errorf("schedule %q has no scheduled time after %v -- cron job %q stays suspended", job.Schedule, dbNow, name)
	}

	tag, err := d.Datastore.Pool.Exec(ctx, `UPDATE cron_job SET suspended = false, next_scheduled_time = $2 WHERE name = $1;`, name, next)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}
	d.Logger.InfoContext(ctx, "cron job unsuspended", "cron_job", name, "next_scheduled_time", next)
	return nil
}

func (d *CronJobDatastore) DeleteCronJob(ctx context.Context, name string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.deleteCronJob(ctx, name)
	})
}

func (d *CronJobDatastore) deleteCronJob(ctx context.Context, name string) error {
	tag, err := d.Datastore.Pool.Exec(ctx, `DELETE FROM cron_job WHERE name = $1;`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", cron.ErrCronJobNotFound, name)
	}
	d.Logger.WarnContext(ctx, "cron job destroyed", "cron_job", name)
	return nil
}

// with N replicas running in different timezones they all have different
// clocks, getting time from db normalizes to a single source.
func (d *CronJobDatastore) dbNow(ctx context.Context, q datastore.Querier) (time.Time, error) {
	var now time.Time
	err := q.QueryRow(ctx, `SELECT now();`).Scan(&now)
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
		&data.Data,
		&data.Metadata,
		&data.NextScheduledTime,
		&data.LastScheduledTime,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &data, nil
}
