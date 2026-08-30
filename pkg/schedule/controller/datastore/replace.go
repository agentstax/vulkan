package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/schedule"
)

// replaceConfig overwrites an already-registered schedule's mutable
// config with declared's: the newest declaration wins.
func (d *ScheduleDatastore) replaceConfig(ctx context.Context, found *ScheduleData, declared *RegisterScheduleData) (*ScheduleData, error) {
	// a scheduled time already due under the old schedule is dropped, not
	// produced late -- the new schedule decides when the schedule next runs
	var next *time.Time
	if found.Expression != declared.Expression.String() {
		seeded, err := d.nextScheduledTime(ctx, d.Datastore.Pool, declared.Expression)
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
		-- vulkan: schedule.replaceConfig
		UPDATE schedule_config
		SET
			expression = $2,
			topic_id = $3,
			concurrency = $4,
			timeout_ns = $5,
			payload = $6,
			schema_version = $7,
			metadata = COALESCE($8, '{}'::jsonb)
		WHERE id = $1;
	`
	tag, err := tx.Exec(ctx, updateConfigSql, found.Id,
		declared.Expression.String(), declared.TopicId, declared.Concurrency, declared.TimeoutNs, declared.Payload, declared.SchemaVersion, declared.Metadata)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, schedule.ErrDeclarationInterrupted.With("schedule", found.Name)
	}

	if next != nil {
		if _, err := tx.Exec(ctx, `
			-- vulkan: schedule.replaceConfig
			UPDATE schedule_cursor SET next_scheduled_at = $2 WHERE schedule_id = $1;
		`, found.Id, *next); err != nil {
			return nil, err
		}
	}

	updated, err := d.get(ctx, tx, found.Name)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, schedule.ErrDeclarationInterrupted.With("schedule", found.Name)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	changes := configChanges(found, updated)
	if len(changes) == 0 {
		d.Logger.InfoContext(ctx, "schedule registered (already existed)", "schedule", updated.Name, "schedule_id", updated.Id)
		return updated, nil
	}

	// the only signal that two services declare this schedule differently
	d.Logger.InfoContext(ctx, "schedule registered (config replaced)",
		append([]any{"schedule", updated.Name, "schedule_id", updated.Id, "next_scheduled_at", updated.NextScheduledAt}, changes...)...)
	return updated, nil
}

// ***************
// *** HELPERS ***
// ***************

// configChanges is every mutable config field the declaration changed, as log
// args. Empty means the declaration matched what was stored.
func configChanges(found *ScheduleData, updated *ScheduleData) []any {
	var changes []any
	if found.Expression != updated.Expression {
		changes = append(changes, "expression", replaced(found.Expression, updated.Expression))
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
