package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
)

// Register resolves declared.Name to its row, creating it owned by
// owner if it doesn't exist. An existing row takes declared's config.
func (d *CronJobDatastore) Register(ctx context.Context, owner *common.Owner, declared *RegisterCronJobData) (*CronJobData, error) {
	var job *CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		job, err = d.register(ctx, owner, declared)
		return err
	})
	return job, err
}

// register registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *CronJobDatastore) register(ctx context.Context, owner *common.Owner, declared *RegisterCronJobData) (*CronJobData, error) {
	found, err := d.get(ctx, d.Datastore.Pool, declared.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceConfig(ctx, found, declared)
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `-- vulkan: cron.register
SELECT pg_advisory_xact_lock(hashtext('cron_job:' || $1));`, declared.Name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.get(ctx, tx, declared.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceConfig(ctx, found, declared)
	}

	next, err := d.nextScheduledTime(ctx, tx, declared.Schedule)
	if err != nil {
		return nil, err
	}

	insertConfigSql := `
		-- vulkan: cron.register
		INSERT INTO cron_job_config (
			system_id,
			topic_id,
			consumer_group_id,
			name,
			schedule,
			concurrency,
			timeout_ns,
			payload,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, '{}'::jsonb), COALESCE($9, '{}'::jsonb))
		RETURNING id;
	`
	var id int64
	if err := tx.QueryRow(ctx, insertConfigSql,
		owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(),
		declared.Name, declared.Schedule.String(), declared.Concurrency, declared.TimeoutNs,
		declared.Payload, declared.Metadata,
	).Scan(&id); err != nil {
		return nil, err
	}

	insertCursorSql := `
		-- vulkan: cron.register
		INSERT INTO cron_job_cursor (cron_job_id, next_scheduled_at)
		VALUES ($1, $2);
	`
	if _, err := tx.Exec(ctx, insertCursorSql, id, next); err != nil {
		return nil, err
	}

	job, err := d.get(ctx, tx, declared.Name)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "cron job registered (created)", "cron_job", job.Name, "cron_job_id", job.Id, "schedule", job.Schedule, "next_scheduled_at", job.NextScheduledAt)
	return job, nil
}
