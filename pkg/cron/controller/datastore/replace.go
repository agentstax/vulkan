package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/cron"
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

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	updateConfigSql := `
		-- vulkan: cron.replaceConfig
		UPDATE cron_job_config
		SET
			schedule = $2,
			concurrency = $3,
			timeout_ns = $4,
			payload = COALESCE($5, '{}'::jsonb),
			metadata = COALESCE($6, '{}'::jsonb)
		WHERE id = $1;
	`
	tag, err := tx.Exec(ctx, updateConfigSql, found.Id,
		declared.Schedule.String(), declared.Concurrency, declared.TimeoutNs, declared.Payload, declared.Metadata)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, cron.ErrDeclarationInterrupted.With("cron_job", found.Name)
	}

	if next != nil {
		if _, err := tx.Exec(ctx, `
			-- vulkan: cron.replaceConfig
			UPDATE cron_job_cursor SET next_scheduled_at = $2 WHERE cron_job_id = $1;
		`, found.Id, *next); err != nil {
			return nil, err
		}
	}

	updated, err := d.get(ctx, tx, found.Name)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, cron.ErrDeclarationInterrupted.With("cron_job", found.Name)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	changes := configChanges(found, updated)
	if len(changes) == 0 {
		d.Logger.InfoContext(ctx, "cron job registered (already existed)", "cron_job", updated.Name, "cron_job_id", updated.Id)
		return updated, nil
	}

	// the only signal that two services declare this job differently
	d.Logger.InfoContext(ctx, "cron job registered (config replaced)",
		append([]any{"cron_job", updated.Name, "cron_job_id", updated.Id, "next_scheduled_at", updated.NextScheduledAt}, changes...)...)
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
	if string(found.Payload) != string(updated.Payload) {
		changes = append(changes, "payload", replaced(string(found.Payload), string(updated.Payload)))
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
