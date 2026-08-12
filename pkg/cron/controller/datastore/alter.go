package datastore

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/cron"
)

// UpdateCronJob applies alter's non-nil fields to the named job.
// Returns (nil, nil) if name is not found.
func (d *CronJobDatastore) UpdateCronJob(ctx context.Context, name string, alter *AlterCronJobData) (*CronJobData, error) {
	var job *CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		job, err = d.updateCronJob(ctx, name, alter)
		return err
	})
	return job, err
}

func (d *CronJobDatastore) updateCronJob(ctx context.Context, name string, alter *AlterCronJobData) (*CronJobData, error) {
	old, err := d.getCronJob(ctx, d.Datastore.Pool, name)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, nil
	}

	// the effective schedule/timeout pair still has to fit
	schedule := alter.Schedule
	if schedule == nil {
		if schedule, err = cron.ParseSchedule(old.Schedule); err != nil {
			return nil, fmt.Errorf("schedule %q: %w", old.Schedule, err)
		}
	}
	timeoutNs := old.TimeoutNs
	if alter.TimeoutNs != nil {
		timeoutNs = *alter.TimeoutNs
	}
	if time.Duration(timeoutNs) > schedule.MinRate() {
		return nil, fmt.Errorf("timeout %v exceeds schedule %q's min rate %v", time.Duration(timeoutNs), schedule, schedule.MinRate())
	}

	var next *time.Time
	if alter.Schedule != nil {
		dbNow, err := d.dbNow(ctx, d.Datastore.Pool)
		if err != nil {
			return nil, err
		}
		n := alter.Schedule.Next(dbNow)
		if n.IsZero() {
			return nil, fmt.Errorf("schedule %q has no scheduled time after %v", alter.Schedule, dbNow)
		}
		next = &n
	}

	// a nil param reaches Postgres as NULL
	// COALESCE keeps the column's current value if nil passed
	sql := `
		UPDATE cron_job
		SET
			schedule = COALESCE($2, schedule),
			concurrency = COALESCE($3, concurrency),
			timeout_ns = COALESCE($4, timeout_ns),
			data = COALESCE($5, data),
			metadata = COALESCE($6, metadata),
			next_scheduled_time = COALESCE($7, next_scheduled_time)
		WHERE name = $1
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
	row := d.Datastore.Pool.QueryRow(ctx, sql,
		name, scheduleExpr(alter.Schedule), alter.Concurrency, alter.TimeoutNs, alter.Data, alter.Metadata, next,
	)
	updated, err := d.scanCronJobData(row)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		// destroyed between the read and the update
		return nil, nil
	}

	d.Logger.InfoContext(ctx, "cron job altered", alterLogFields(old, updated)...)
	return updated, nil
}

// scheduleExpr widens *cron.Schedule to the *string the schedule column
// stores, passing nil through so COALESCE sees NULL.
func scheduleExpr(schedule *cron.Schedule) *string {
	if schedule == nil {
		return nil
	}
	expr := schedule.String()
	return &expr
}

// alterLogFields renders old -> new pairs for just the fields that changed.
func alterLogFields(old, updated *CronJobData) []any {
	fields := []any{"cron_job", updated.Name, "cron_job_id", updated.Id}

	if old.Schedule != updated.Schedule {
		fields = append(fields, "schedule", fmt.Sprintf("%s -> %s", old.Schedule, updated.Schedule))
	}
	if old.Concurrency != updated.Concurrency {
		fields = append(fields, "concurrency", fmt.Sprintf("%s -> %s", old.Concurrency, updated.Concurrency))
	}
	if old.TimeoutNs != updated.TimeoutNs {
		fields = append(fields, "timeout", fmt.Sprintf("%v -> %v", time.Duration(old.TimeoutNs), time.Duration(updated.TimeoutNs)))
	}
	if !bytes.Equal(old.Data, updated.Data) {
		fields = append(fields, "data", fmt.Sprintf("%s -> %s", old.Data, updated.Data))
	}
	if !bytes.Equal(old.Metadata, updated.Metadata) {
		fields = append(fields, "metadata", fmt.Sprintf("%s -> %s", old.Metadata, updated.Metadata))
	}
	if !old.NextScheduledTime.Equal(updated.NextScheduledTime) {
		fields = append(fields, "next_scheduled_time", fmt.Sprintf("%v -> %v", old.NextScheduledTime, updated.NextScheduledTime))
	}
	return fields
}
