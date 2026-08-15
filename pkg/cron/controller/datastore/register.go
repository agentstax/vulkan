package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// RegisterCronJob resolves register.Name to its row, creating it owned by
// owner if it doesn't exist. An existing row takes register's config.
func (d *CronJobDatastore) RegisterCronJob(ctx context.Context, owner *common.Owner, register *RegisterCronJobData) (*CronJobData, error) {
	var job *CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		job, err = d.registerCronJob(ctx, owner, register)
		return err
	})
	return job, err
}

// registerCronJob registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *CronJobDatastore) registerCronJob(ctx context.Context, owner *common.Owner, register *RegisterCronJobData) (*CronJobData, error) {
	found, err := d.getCronJob(ctx, d.Datastore.Pool, register.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceCronJobConfig(ctx, found, register)
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('cron_job:' || $1));`, register.Name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.getCronJob(ctx, tx, register.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceCronJobConfig(ctx, found, register)
	}

	dbNow, err := d.dbNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	next := register.Schedule.Next(dbNow)
	if next.IsZero() {
		return nil, fmt.Errorf("schedule %q has no scheduled time after %v", register.Schedule, dbNow)
	}

	insertSql := `
		INSERT INTO cron_job (
			system_id,
			topic_id,
			consumer_group_id,
			name,
			schedule,
			concurrency,
			timeout_ns,
			data,
			metadata,
			next_scheduled_time
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, '{}'::jsonb), COALESCE($9, '{}'::jsonb), $10)
		RETURNING
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
			last_scheduled_time;
	`
	job, err := d.scanCronJobData(tx.QueryRow(ctx, insertSql,
		owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(),
		register.Name, register.Schedule.String(), register.Concurrency, register.TimeoutNs,
		register.Data, register.Metadata, next,
	))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "cron job registered (created)", "cron_job", job.Name, "cron_job_id", job.Id, "schedule", job.Schedule, "next_scheduled_time", job.NextScheduledTime)
	return job, nil
}
