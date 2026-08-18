package datastore

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

// DueCronJobs lists the cron jobs with a due scheduled time. Unlocked -- each
// row is rechecked under its own lock before its JobRequest is produced.
func (d *CronSchedulerDatastore) DueCronJobs(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		ids, err = d.dueCronJobs(ctx)
		return err
	})
	return ids, err
}

func (d *CronSchedulerDatastore) dueCronJobs(ctx context.Context) ([]int64, error) {
	sql := `
		SELECT id FROM cron_job
		WHERE next_scheduled_time <= now() AND NOT suspended
		ORDER BY next_scheduled_time;
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

// ClaimDueCronJob rereads the row under the caller's transaction lock,
// making the unlocked due scan safe -- nil means it raced away (suspended,
// destroyed, or another scheduler's transaction holds it).
// Runs inside the produce transaction -- no retry, the transaction owns its
// own error handling.
func (d *CronSchedulerDatastore) ClaimDueCronJob(ctx context.Context, q datastore.Querier, id int64) (*DueCronJobData, error) {
	return d.claimDueCronJob(ctx, q, id)
}

func (d *CronSchedulerDatastore) claimDueCronJob(ctx context.Context, q datastore.Querier, id int64) (*DueCronJobData, error) {
	sql := `
		SELECT id, name, schedule, concurrency, timeout_ns, data, metadata, next_scheduled_time, now()
		FROM cron_job
		WHERE id = $1 AND next_scheduled_time <= now() AND NOT suspended
		FOR UPDATE SKIP LOCKED;
	`
	var data DueCronJobData
	var timeoutNs int64
	err := q.QueryRow(ctx, sql, id).Scan(&data.Id, &data.Name, &data.Schedule, &data.Concurrency,
		&timeoutNs, &data.Data, &data.Metadata, &data.NextScheduledTime, &data.DbNow)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	data.Timeout = time.Duration(timeoutNs)
	return &data, nil
}

// AdvanceCronJob moves the produced row to its next scheduled time, in the
// caller's producing transaction -- no retry, the transaction owns its own
// error handling.
func (d *CronSchedulerDatastore) AdvanceCronJob(ctx context.Context, q datastore.Querier, id int64, next time.Time, produced time.Time) error {
	return d.advanceCronJob(ctx, q, id, next, produced)
}

func (d *CronSchedulerDatastore) advanceCronJob(ctx context.Context, q datastore.Querier, id int64, next time.Time, produced time.Time) error {
	_, err := q.Exec(ctx, `UPDATE cron_job SET next_scheduled_time = $2, last_scheduled_time = $3 WHERE id = $1;`, id, next, produced)
	return err
}

// SuspendCronJob sets the row suspended, in the caller's producing transaction
// -- next_scheduled_time is NOT NULL and an unsatisfiable schedule has no
// honest value for it. No retry, the transaction owns its own error handling.
func (d *CronSchedulerDatastore) SuspendCronJob(ctx context.Context, q datastore.Querier, id int64, produced time.Time) error {
	return d.suspendCronJob(ctx, q, id, produced)
}

func (d *CronSchedulerDatastore) suspendCronJob(ctx context.Context, q datastore.Querier, id int64, produced time.Time) error {
	_, err := q.Exec(ctx, `UPDATE cron_job SET suspended = true, last_scheduled_time = $2 WHERE id = $1;`, id, produced)
	return err
}
