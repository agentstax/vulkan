package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Get resolves a topic by name. Returns (nil, nil) if name is not found.
func (d *TopicDatastore) Get(ctx context.Context, name string) (*TopicConfigRow, error) {
	var topicConfigRow *TopicConfigRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topicConfigRow, err = d.get(ctx, d.Datastore.Pool, name)
		return err
	})
	return topicConfigRow, err
}

func (d *TopicDatastore) get(ctx context.Context, q datastore.Querier, name string) (*TopicConfigRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: topic.get
		SELECT
			id,
			system_id,
			name,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at
		FROM %[1]s.topic_config
		WHERE name = $1;
	`, d.Datastore.Schema)
	return d.scanTopicConfigRow(q.QueryRow(ctx, sql, name))
}

// GetById resolves a topic by its id. Returns (nil, nil) if no topic has it.
func (d *TopicDatastore) GetById(ctx context.Context, id int64) (*TopicConfigRow, error) {
	var topicConfigRow *TopicConfigRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topicConfigRow, err = d.getById(ctx, id)
		return err
	})
	return topicConfigRow, err
}

func (d *TopicDatastore) getById(ctx context.Context, id int64) (*TopicConfigRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: topic.getById
		SELECT
			id,
			system_id,
			name,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at
		FROM %[1]s.topic_config
		WHERE id = $1;
	`, d.Datastore.Schema)
	return d.scanTopicConfigRow(d.Datastore.Pool.QueryRow(ctx, sql, id))
}

func (d *TopicDatastore) List(ctx context.Context) ([]TopicConfigRow, error) {
	var topics []TopicConfigRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topics, err = d.list(ctx)
		return err
	})
	return topics, err
}

func (d *TopicDatastore) list(ctx context.Context) ([]TopicConfigRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: topic.list
		SELECT
			id,
			system_id,
			name,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at
		FROM %[1]s.topic_config
		ORDER BY name;
	`, d.Datastore.Schema)
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		// 42P01 = table does not exist -- an unregistered database has no topics
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var topics []TopicConfigRow
	for rows.Next() {
		topicConfigRow, err := d.scanTopicConfigRow(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, *topicConfigRow)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return topics, nil
}

// Register resolves declared's name to its row, creating
// it (and its per-topic tables) if it doesn't exist. An existing row takes
// declared's mutable config; its partition_size must match.
func (d *TopicDatastore) Register(ctx context.Context, declared *TopicConfigRow, declaredBy string) (*TopicConfigRow, error) {
	var registered *TopicConfigRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		registered, err = d.register(ctx, declared, declaredBy)
		return err
	})
	return registered, err
}

// register registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *TopicDatastore) register(ctx context.Context, declared *TopicConfigRow, declaredBy string) (*TopicConfigRow, error) {
	// private get, not Get -- otherwise would have nested retries.
	found, err := d.get(ctx, d.Datastore.Pool, declared.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceConfig(ctx, found, declared, declaredBy)
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	lockKey, err := common.NewAdvisoryLockKey("topic", d.Datastore.Schema, declared.Name)
	if err != nil {
		return nil, err
	}

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `
		-- vulkan: topic.register
		SELECT pg_advisory_xact_lock($1);
	`, lockKey.Value()); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.get(ctx, tx, declared.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceConfig(ctx, found, declared, declaredBy)
	}

	insertSql := fmt.Sprintf(`
		-- vulkan: topic.register
		INSERT INTO %[1]s.topic_config (system_id, name, partition_size, retention_ttl_ns, allow_drop_past_committed, idempotency_key_ttl_ns, delivery_log_mode)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at;
	`, d.Datastore.Schema)
	created := *declared
	if err := tx.QueryRow(ctx, insertSql, declared.SystemId, declared.Name, declared.PartitionSize, declared.RetentionTTLNs, declared.AllowDropPastCommitted, declared.IdempotencyKeyTTLNs, declared.DeliveryLogMode).
		Scan(&created.Id, &created.CreatedAt, &created.UpdatedAt); err != nil {
		return nil, err
	}

	if err := d.appendTopicConfigLog(ctx, tx, &created, declaredBy); err != nil {
		return nil, err
	}

	if err := d.createTopicTables(ctx, tx, created.Id, declared.PartitionSize); err != nil {
		return nil, err
	}

	// add the migration baseline in the SAME txn
	migrationSql := fmt.Sprintf(`
		-- vulkan: topic.register
		INSERT INTO %[1]s.migration_log (topic_id, migration_version, status)
		VALUES ($1, 1, 'success');
	`, d.Datastore.Schema)
	if _, err := tx.Exec(ctx, migrationSql, created.Id); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "topic registered (created)", "topic", created.Name, "topic_id", created.Id)
	return &created, nil
}

// Rename moves the topic under oldName to newName, appending its
// topic_config_log row beside the update.
// Returns (nil, nil) if no topic is registered under oldName
// ErrTopicNameTaken if newName is already registered.
func (d *TopicDatastore) Rename(ctx context.Context, oldName string, newName string, declaredBy string) (*TopicConfigRow, error) {
	var renamed *TopicConfigRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		renamed, err = d.rename(ctx, oldName, newName, declaredBy)
		return err
	})
	return renamed, err
}

func (d *TopicDatastore) rename(ctx context.Context, oldName string, newName string, declaredBy string) (*TopicConfigRow, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	sql := fmt.Sprintf(`
		-- vulkan: topic.rename
		UPDATE %[1]s.topic_config
		SET name = $2, updated_at = NOW()
		WHERE name = $1
		RETURNING
			id,
			system_id,
			name,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at;
	`, d.Datastore.Schema)
	renamed, err := d.scanTopicConfigRow(tx.QueryRow(ctx, sql, oldName, newName))
	if err != nil {
		// 23505 = unique constraint violation ie name taken
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, topic.ErrTopicNameTaken.With("topic", newName)
		}
		return nil, err
	}
	if renamed == nil {
		return nil, nil
	}

	if err := d.appendTopicConfigLog(ctx, tx, renamed, declaredBy); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "topic renamed", "topic", oldName, "new_name", newName, "topic_id", renamed.Id)
	return renamed, nil
}

// appendTopicConfigLog writes data's full snapshot as one topic_config_log row, inside
// the transaction that changed the topic row.
func (d *TopicDatastore) appendTopicConfigLog(ctx context.Context, q datastore.Querier, data *TopicConfigRow, declaredBy string) error {
	sql := fmt.Sprintf(`
		-- vulkan: topic.appendTopicConfigLog
		INSERT INTO %[1]s.topic_config_log (topic_id, name, partition_size, retention_ttl_ns, allow_drop_past_committed, idempotency_key_ttl_ns, delivery_log_mode, declared_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
	`, d.Datastore.Schema)
	_, err := q.Exec(ctx, sql, data.Id, data.Name, data.PartitionSize, data.RetentionTTLNs, data.AllowDropPastCommitted, data.IdempotencyKeyTTLNs, data.DeliveryLogMode, declaredBy)
	return err
}

// scanTopicConfigRow scans a row shaped like getTopic's SELECT -- the column list
// every one of those queries shares. Returns (nil, nil) when the row -- or
// topic_config itself, 42P01 -- isn't there yet.
func (d *TopicDatastore) scanTopicConfigRow(row pgx.Row) (*TopicConfigRow, error) {
	var data TopicConfigRow
	err := row.Scan(
		&data.Id,
		&data.SystemId,
		&data.Name,
		&data.PartitionSize,
		&data.RetentionTTLNs,
		&data.AllowDropPastCommitted,
		&data.IdempotencyKeyTTLNs,
		&data.DeliveryLogMode,
		&data.CreatedAt,
		&data.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		// 42P01 = table does not exist
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil, nil
		}
		return nil, err
	}
	return &data, nil
}
