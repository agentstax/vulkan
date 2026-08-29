package datastore

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

// ListDue lists the cron jobs with a due scheduled time. Unlocked -- each
// row is rechecked under its own lock before its JobRequest is produced.
func (d *CronSchedulerDatastore) ListDue(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		ids, err = d.listDue(ctx)
		return err
	})
	return ids, err
}

func (d *CronSchedulerDatastore) listDue(ctx context.Context) ([]int64, error) {
	sql := `
		-- vulkan: cronscheduler.listDue
		SELECT cron_job_config.id
		FROM cron_job_cursor
		JOIN cron_job_config ON cron_job_config.id = cron_job_cursor.cron_job_id
		WHERE cron_job_cursor.next_scheduled_at <= now() AND NOT cron_job_config.suspended
		ORDER BY cron_job_cursor.next_scheduled_at;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClaimDue rereads the row under the caller's transaction lock,
// making the unlocked due scan safe -- nil means it raced away (suspended,
// destroyed, or another scheduler's transaction holds it).
// Runs inside the produce transaction -- no retry, the transaction owns its
// own error handling.
func (d *CronSchedulerDatastore) ClaimDue(ctx context.Context, q datastore.Querier, id int64) (*DueCronJobData, error) {
	return d.claimDue(ctx, q, id)
}

func (d *CronSchedulerDatastore) claimDue(ctx context.Context, q datastore.Querier, id int64) (*DueCronJobData, error) {
	// FOR UPDATE locks both joined rows -- the cursor row Advance writes and
	// the config row Suspend writes
	sql := `
		-- vulkan: cronscheduler.claimDue
		SELECT
			cron_job_config.id,
			cron_job_config.name,
			cron_job_config.schedule,
			cron_job_config.concurrency,
			cron_job_config.timeout_ns,
			cron_job_config.payload,
			cron_job_config.metadata,
			cron_job_cursor.next_scheduled_at,
			now()
		FROM cron_job_cursor
		JOIN cron_job_config ON cron_job_config.id = cron_job_cursor.cron_job_id
		WHERE cron_job_config.id = $1
			AND cron_job_cursor.next_scheduled_at <= now()
			AND NOT cron_job_config.suspended
		FOR UPDATE SKIP LOCKED;
	`
	var data DueCronJobData
	var timeoutNs int64
	err := q.QueryRow(ctx, sql, id).Scan(&data.Id, &data.Name, &data.Schedule, &data.Concurrency,
		&timeoutNs, &data.Payload, &data.Metadata, &data.NextScheduledAt, &data.DbNow)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	data.Timeout = time.Duration(timeoutNs)
	return &data, nil
}

// Advance moves the produced row to its next scheduled time, in the
// caller's producing transaction -- no retry, the transaction owns its own
// error handling.
func (d *CronSchedulerDatastore) Advance(ctx context.Context, q datastore.Querier, id int64, next time.Time, produced time.Time) error {
	return d.advance(ctx, q, id, next, produced)
}

func (d *CronSchedulerDatastore) advance(ctx context.Context, q datastore.Querier, id int64, next time.Time, produced time.Time) error {
	_, err := q.Exec(ctx, `
		-- vulkan: cronscheduler.advance
		UPDATE cron_job_cursor SET next_scheduled_at = $2, last_scheduled_at = $3 WHERE cron_job_id = $1;
	`, id, next, produced)
	return err
}

// Suspend sets the row suspended, in the caller's producing transaction
// -- next_scheduled_at is NOT NULL and an unsatisfiable schedule has no
// honest value for it. No retry, the transaction owns its own error handling.
func (d *CronSchedulerDatastore) Suspend(ctx context.Context, q datastore.Querier, id int64, produced time.Time) error {
	return d.suspend(ctx, q, id, produced)
}

func (d *CronSchedulerDatastore) suspend(ctx context.Context, q datastore.Querier, id int64, produced time.Time) error {
	if _, err := q.Exec(ctx, `
		-- vulkan: cronscheduler.suspend
		UPDATE cron_job_config SET suspended = true WHERE id = $1;
	`, id); err != nil {
		return err
	}

	_, err := q.Exec(ctx, `
		-- vulkan: cronscheduler.suspend
		UPDATE cron_job_cursor SET last_scheduled_at = $2 WHERE cron_job_id = $1;
	`, id, produced)
	return err
}
