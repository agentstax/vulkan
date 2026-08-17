package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// GetTopic resolves topic (name, schemaVersion).
// Returns (nil, nil) if (name, schemaVersion) is not found.
func (d *TopicDatastore) GetTopic(ctx context.Context, name string, schemaVersion int64) (*TopicData, error) {
	var topicData *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topicData, err = d.getTopic(ctx, d.Datastore.Pool, name, schemaVersion)
		return err
	})
	return topicData, err
}

func (d *TopicDatastore) getTopic(ctx context.Context, q datastore.Querier, name string, schemaVersion int64) (*TopicData, error) {
	sql := `
		SELECT
			id,
			system_id,
			name,
			schema_version,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at
		FROM topic
		WHERE name = $1 AND schema_version = $2;
	`
	return d.scanTopicData(q.QueryRow(ctx, sql, name, schemaVersion))
}

// GetTopicById resolves a topic by its id. Returns (nil, nil) if no topic has it.
func (d *TopicDatastore) GetTopicById(ctx context.Context, id int64) (*TopicData, error) {
	var topicData *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topicData, err = d.getTopicById(ctx, id)
		return err
	})
	return topicData, err
}

func (d *TopicDatastore) getTopicById(ctx context.Context, id int64) (*TopicData, error) {
	sql := `
		SELECT
			id,
			system_id,
			name,
			schema_version,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at
		FROM topic
		WHERE id = $1;
	`
	return d.scanTopicData(d.Datastore.Pool.QueryRow(ctx, sql, id))
}

func (d *TopicDatastore) ListTopics(ctx context.Context) ([]TopicData, error) {
	var topics []TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topics, err = d.listTopics(ctx)
		return err
	})
	return topics, err
}

func (d *TopicDatastore) listTopics(ctx context.Context) ([]TopicData, error) {
	sql := `
		SELECT
			id,
			system_id,
			name,
			schema_version,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at
		FROM topic
		ORDER BY name, schema_version;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []TopicData
	for rows.Next() {
		topicData, err := d.scanTopicData(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, *topicData)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return topics, nil
}

// RegisterTopic resolves declared's (name, schema_version) to its row, creating
// it (and its per-topic tables) if it doesn't exist. An existing row takes
// declared's mutable config; its partition_size must match.
func (d *TopicDatastore) RegisterTopic(ctx context.Context, declared *TopicData) (*TopicData, error) {
	var registered *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		registered, err = d.registerTopic(ctx, declared)
		return err
	})
	return registered, err
}

// registerTopic registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *TopicDatastore) registerTopic(ctx context.Context, declared *TopicData) (*TopicData, error) {
	// private getTopic, not GetTopic -- otherwise would have nested retries.
	found, err := d.getTopic(ctx, d.Datastore.Pool, declared.Name, declared.SchemaVersion)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceTopicConfig(ctx, found, declared)
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('topic:' || $1));`, declared.Name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.getTopic(ctx, tx, declared.Name, declared.SchemaVersion)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceTopicConfig(ctx, found, declared)
	}

	insertSql := `
		INSERT INTO topic (system_id, name, schema_version, partition_size, retention_ttl_ns, allow_drop_past_committed, idempotency_key_ttl_ns, delivery_log_mode)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at;
	`
	created := *declared
	if err := tx.QueryRow(ctx, insertSql, declared.SystemId, declared.Name, declared.SchemaVersion, declared.PartitionSize, declared.RetentionTTLNs, declared.AllowDropPastCommitted, declared.IdempotencyKeyTTLNs, declared.DeliveryLogMode).
		Scan(&created.Id, &created.CreatedAt, &created.UpdatedAt); err != nil {
		return nil, err
	}

	if err := d.createTopicTables(ctx, tx, created.Id, declared.PartitionSize); err != nil {
		return nil, err
	}

	// add the migration baseline in the SAME txn
	migrationSql := `
		INSERT INTO migration_log (topic_id, migration_version, status)
		VALUES ($1, 1, 'success');
	`
	if _, err := tx.Exec(ctx, migrationSql, created.Id); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "topic registered (created)", "topic", created.Name, "topic_id", created.Id, "schema_version", created.SchemaVersion)
	return &created, nil
}

// RenameTopic moves every version under oldName to newName in one statement.
// Returns (nil, nil) if no version is registered under oldName
// ErrTopicNameTaken if newName already has any (name, version) registered.
func (d *TopicDatastore) RenameTopic(ctx context.Context, oldName string, newName string) ([]TopicData, error) {
	var topics []TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topics, err = d.renameTopic(ctx, oldName, newName)
		return err
	})
	return topics, err
}

func (d *TopicDatastore) renameTopic(ctx context.Context, oldName string, newName string) ([]TopicData, error) {
	sql := `
		UPDATE topic
		SET name = $2, updated_at = NOW()
		WHERE name = $1
		RETURNING
			id,
			system_id,
			name,
			schema_version,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			delivery_log_mode,
			created_at,
			updated_at;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql, oldName, newName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []TopicData
	for rows.Next() {
		topicData, err := d.scanTopicData(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, *topicData)
	}
	if err := rows.Err(); err != nil {
		// 23505 = unqiue constraint violation ie name taken
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, fmt.Errorf("%w: %s", topic.ErrTopicNameTaken, newName)
		}
		return nil, err
	}
	if len(topics) == 0 {
		return nil, nil
	}

	d.Logger.InfoContext(ctx, "topic family renamed", "versions", len(topics), "name", fmt.Sprintf("%s -> %s", oldName, newName))
	return topics, nil
}

// scanTopicData scans a row shaped like getTopic's SELECT -- the column list
// every one of those queries shares.
func (d *TopicDatastore) scanTopicData(row pgx.Row) (*TopicData, error) {
	var data TopicData
	err := row.Scan(
		&data.Id,
		&data.SystemId,
		&data.Name,
		&data.SchemaVersion,
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
		return nil, err
	}
	return &data, nil
}
