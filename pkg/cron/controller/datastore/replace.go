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

	changes := configChanges(found, declared, dataJson, metadataJson)
	if len(changes) == 0 {
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
		append([]any{"cron_job", updated.Name, "cron_job_id", updated.Id, "next_scheduled_time", updated.NextScheduledTime}, changes...)...)
	return updated, nil
}

// configChanges is every mutable config field the declaration would change,
// as log args. Empty means the declaration matches what is stored.
func configChanges(found *CronJobData, declared *RegisterCronJobData, dataJson json.RawMessage, metadataJson json.RawMessage) []any {
	var changes []any
	if found.Schedule != declared.Schedule.String() {
		changes = append(changes, "schedule", replaced(found.Schedule, declared.Schedule))
	}
	if found.Concurrency != declared.Concurrency {
		changes = append(changes, "concurrency", replaced(found.Concurrency, declared.Concurrency))
	}
	if found.TimeoutNs != declared.TimeoutNs {
		changes = append(changes, "timeout", replaced(time.Duration(found.TimeoutNs), time.Duration(declared.TimeoutNs)))
	}
	if !jsonEqual(found.Data, dataJson) {
		changes = append(changes, "data", replaced(string(found.Data), string(dataJson)))
	}
	if !jsonEqual(found.Metadata, metadataJson) {
		changes = append(changes, "metadata", replaced(string(found.Metadata), string(metadataJson)))
	}
	return changes
}

// replaced renders one field's change as the log line carries it: old -> new.
func replaced(stored any, declared any) string {
	return fmt.Sprintf("%v -> %v", stored, declared)
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
