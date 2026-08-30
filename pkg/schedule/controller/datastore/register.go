package datastore

import (
	"context"
)

// Register resolves declared.Name to its row, creating it if it doesn't
// exist. An existing row takes declared's config.
func (d *ScheduleDatastore) Register(ctx context.Context, declared *RegisterScheduleData) (*ScheduleData, error) {
	var found *ScheduleData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		found, err = d.register(ctx, declared)
		return err
	})
	return found, err
}

// register registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *ScheduleDatastore) register(ctx context.Context, declared *RegisterScheduleData) (*ScheduleData, error) {
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
	if _, err := tx.Exec(ctx, `
		-- vulkan: schedule.register
		SELECT pg_advisory_xact_lock(hashtext('schedule:' || $1));
	`, declared.Name); err != nil {
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

	next, err := d.nextScheduledTime(ctx, tx, declared.Expression)
	if err != nil {
		return nil, err
	}

	insertConfigSql := `
		-- vulkan: schedule.register
		INSERT INTO schedule_config (
			system_id,
			topic_id,
			name,
			expression,
			concurrency,
			timeout_ns,
			payload,
			schema_version,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9, '{}'::jsonb))
		RETURNING id;
	`
	var id int64
	if err := tx.QueryRow(ctx, insertConfigSql,
		declared.SystemId, declared.TopicId,
		declared.Name, declared.Expression.String(), declared.Concurrency, declared.TimeoutNs,
		declared.Payload, declared.SchemaVersion, declared.Metadata,
	).Scan(&id); err != nil {
		return nil, err
	}

	insertCursorSql := `
		-- vulkan: schedule.register
		INSERT INTO schedule_cursor (schedule_id, next_scheduled_at)
		VALUES ($1, $2);
	`
	if _, err := tx.Exec(ctx, insertCursorSql, id, next); err != nil {
		return nil, err
	}

	created, err := d.get(ctx, tx, declared.Name)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "schedule registered (created)", "schedule", created.Name, "schedule_id", created.Id, "expression", created.Expression, "next_scheduled_at", created.NextScheduledAt)
	return created, nil
}
