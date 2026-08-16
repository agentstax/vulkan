package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// replaceCronJobConfig overwrites an already-registered cron job's mutable
// config with declared's: the newest declaration wins.
func (d *CronJobDatastore) replaceCronJobConfig(ctx context.Context, found *CronJobData, declared *RegisterCronJobData) (*CronJobData, error) {
	dataJson, err := marshalJson(declared.Data)
	if err != nil {
		return nil, fmt.Errorf("data: %w", err)
	}
	metadataJson, err := marshalJson(declared.Metadata)
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}

	if !configDiffers(found, declared, dataJson, metadataJson) {
		d.Logger.InfoContext(ctx, "cron job registered (already existed)", "cron_job", found.Name, "cron_job_id", found.Id)
		return found, nil
	}

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
			data = $5,
			metadata = $6,
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
		declared.Schedule.String(), declared.Concurrency, declared.TimeoutNs, dataJson, metadataJson, next)
	updated, err := d.scanCronJobData(row)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, fmt.Errorf("cron job %q was deleted while its declaration was in flight -- rerun the declaration if it should still exist", found.Name)
	}

	// the only signal that two services declare this job differently
	d.Logger.InfoContext(ctx, "cron job registered (config replaced)",
		"cron_job", updated.Name,
		"cron_job_id", updated.Id,
		"schedule", updated.Schedule,
		"concurrency", updated.Concurrency,
		"timeout", time.Duration(updated.TimeoutNs),
		"data", string(updated.Data),
		"metadata", string(updated.Metadata),
		"next_scheduled_time", updated.NextScheduledTime)
	return updated, nil
}

// configDiffers reports whether the declaration would change any mutable
// config field.
func configDiffers(found *CronJobData, declared *RegisterCronJobData, dataJson json.RawMessage, metadataJson json.RawMessage) bool {
	return found.Schedule != declared.Schedule.String() ||
		found.Concurrency != declared.Concurrency ||
		found.TimeoutNs != declared.TimeoutNs ||
		!jsonEqual(found.Data, dataJson) ||
		!jsonEqual(found.Metadata, metadataJson)
}

// marshalJson mirrors what the INSERT stores: nil -> {} (its COALESCE).
func marshalJson(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage(`{}`), nil
	}
	return json.Marshal(value)
}

// jsonEqual matches jsonb's = -- the stored side comes back normalized, so
// key order and whitespace can't count.
func jsonEqual(stored json.RawMessage, declared json.RawMessage) bool {
	var storedValue, declaredValue any
	if json.Unmarshal(stored, &storedValue) != nil || json.Unmarshal(declared, &declaredValue) != nil {
		return false
	}
	return reflect.DeepEqual(storedValue, declaredValue)
}
