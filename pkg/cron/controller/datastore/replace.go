package datastore

import (
	"context"
	"fmt"
	"time"
)

// replaceConfig overwrites an already-registered cron job's mutable
// config with declared's: the newest declaration wins.
func (d *CronJobDatastore) replaceConfig(ctx context.Context, found *CronJobData, declared *RegisterCronJobData) (*CronJobData, error) {
	// a scheduled time already due under the old schedule is dropped, not
	// produced late -- the new schedule decides when the job next runs
	var next *time.Time
	if found.Schedule != declared.Schedule.String() {
		seeded, err := d.nextScheduledTime(ctx, d.Datastore.Pool, declared.Schedule)
		if err != nil {
			return nil, err
		}
		next = &seeded
	}

	sql := `
		UPDATE cron_job
		SET
			schedule = $2,
			concurrency = $3,
			timeout_ns = $4,
			data = COALESCE($5, '{}'::jsonb),
			metadata = COALESCE($6, '{}'::jsonb),
			next_scheduled_time = COALESCE($7, next_scheduled_time)
		WHERE id = $1
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
	row := d.Datastore.Pool.QueryRow(ctx, sql, found.Id,
		declared.Schedule.String(), declared.Concurrency, declared.TimeoutNs, declared.Data, declared.Metadata, next)
	updated, err := d.scanCronJobData(row)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, fmt.Errorf("cron job %q was deleted while its declaration was in flight -- rerun the declaration if it should still exist", found.Name)
	}

	changes := configChanges(found, updated)
	if len(changes) == 0 {
		d.Logger.InfoContext(ctx, "cron job registered (already existed)", "cron_job", updated.Name, "cron_job_id", updated.Id)
		return updated, nil
	}

	// the only signal that two services declare this job differently
	d.Logger.InfoContext(ctx, "cron job registered (config replaced)",
		append([]any{"cron_job", updated.Name, "cron_job_id", updated.Id, "next_scheduled_time", updated.NextScheduledTime}, changes...)...)
	return updated, nil
}

// ***************
// *** HELPERS ***
// ***************

// configChanges is every mutable config field the declaration changed, as log
// args. Empty means the declaration matched what was stored.
func configChanges(found *CronJobData, updated *CronJobData) []any {
	var changes []any
	if found.Schedule != updated.Schedule {
		changes = append(changes, "schedule", replaced(found.Schedule, updated.Schedule))
	}
	if found.Concurrency != updated.Concurrency {
		changes = append(changes, "concurrency", replaced(found.Concurrency, updated.Concurrency))
	}
	if found.TimeoutNs != updated.TimeoutNs {
		changes = append(changes, "timeout", replaced(time.Duration(found.TimeoutNs), time.Duration(updated.TimeoutNs)))
	}

	// both sides are jsonb-normalized by the database, so equal values print
	// identical text
	if string(found.Data) != string(updated.Data) {
		changes = append(changes, "data", replaced(string(found.Data), string(updated.Data)))
	}
	if string(found.Metadata) != string(updated.Metadata) {
		changes = append(changes, "metadata", replaced(string(found.Metadata), string(updated.Metadata)))
	}
	return changes
}

// replaced renders one field's change as the log line carries it: old -> new.
func replaced(stored any, declared any) string {
	return fmt.Sprintf("%v -> %v", stored, declared)
}
