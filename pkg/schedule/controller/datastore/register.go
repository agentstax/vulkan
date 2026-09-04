package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/schedule"
)

// Register resolves name to its row, creating it if it doesn't exist. An
// existing row takes the supplied config values.
func (d *ScheduleDatastore) Register(ctx context.Context, systemId int64, topicId int64, name string, expression *schedule.ScheduleExpression, concurrency common.ConcurrencyPolicy, timeout time.Duration, payload any, schemaVersion int, metadata any) (*ScheduleConfigRow, error) {
	var found *ScheduleConfigRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		found, err = d.register(ctx, systemId, topicId, name, expression, concurrency, timeout, payload, schemaVersion, metadata)
		return err
	})
	return found, err
}

// register registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *ScheduleDatastore) register(ctx context.Context, systemId int64, topicId int64, name string, expression *schedule.ScheduleExpression, concurrency common.ConcurrencyPolicy, timeout time.Duration, payload any, schemaVersion int, metadata any) (*ScheduleConfigRow, error) {
	found, err := d.get(ctx, d.Datastore.Pool, name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceConfig(ctx, found, topicId, expression, concurrency, timeout, payload, schemaVersion, metadata)
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	lockKey, err := common.NewAdvisoryLockKey("schedule", d.Datastore.Schema, name)
	if err != nil {
		return nil, err
	}

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `
		-- vulkan: schedule.register
		SELECT pg_advisory_xact_lock($1);
	`, lockKey.Value()); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.get(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceConfig(ctx, found, topicId, expression, concurrency, timeout, payload, schemaVersion, metadata)
	}

	next, err := d.nextScheduledTime(ctx, tx, expression)
	if err != nil {
		return nil, err
	}

	insertConfigSql := fmt.Sprintf(`
		-- vulkan: schedule.register
		INSERT INTO %[1]s.schedule_config (
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
	`, d.Datastore.Schema)
	var id int64
	if err := tx.QueryRow(ctx, insertConfigSql,
		systemId, topicId,
		name, expression.String(), string(concurrency), int64(timeout),
		payload, schemaVersion, metadata,
	).Scan(&id); err != nil {
		return nil, err
	}

	insertCursorSql := fmt.Sprintf(`
		-- vulkan: schedule.register
		INSERT INTO %[1]s.schedule_cursor (schedule_id, next_scheduled_at)
		VALUES ($1, $2);
	`, d.Datastore.Schema)
	if _, err := tx.Exec(ctx, insertCursorSql, id, next); err != nil {
		return nil, err
	}

	created, err := d.get(ctx, tx, name)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "schedule registered (created)", "schedule", created.Name, "schedule_id", created.Id, "expression", created.Expression, "next_scheduled_at", created.NextScheduledAt)
	return created, nil
}
