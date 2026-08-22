package datastore

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Get resolves topic (name, schemaVersion).
// Returns (nil, nil) if (name, schemaVersion) is not found.
func (d *TopicDatastore) Get(ctx context.Context, name string, schemaVersion int64) (*TopicData, error) {
	var topicData *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topicData, err = d.get(ctx, d.Datastore.Pool, name, schemaVersion)
		return err
	})
	return topicData, err
}

func (d *TopicDatastore) get(ctx context.Context, q datastore.Querier, name string, schemaVersion int64) (*TopicData, error) {
	sql := `
		-- vulkan: topic.get
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

// GetById resolves a topic by its id. Returns (nil, nil) if no topic has it.
func (d *TopicDatastore) GetById(ctx context.Context, id int64) (*TopicData, error) {
	var topicData *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topicData, err = d.getById(ctx, id)
		return err
	})
	return topicData, err
}

func (d *TopicDatastore) getById(ctx context.Context, id int64) (*TopicData, error) {
	sql := `
		-- vulkan: topic.getById
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

func (d *TopicDatastore) List(ctx context.Context) ([]TopicData, error) {
	var topics []TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topics, err = d.list(ctx)
		return err
	})
	return topics, err
}

func (d *TopicDatastore) list(ctx context.Context) ([]TopicData, error) {
	sql := `
		-- vulkan: topic.list
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

// Register resolves declared's (name, schema_version) to its row, creating
// it (and its per-topic tables) if it doesn't exist. An existing row takes
// declared's mutable config; its partition_size must match.
func (d *TopicDatastore) Register(ctx context.Context, declared *TopicData, declaredBy string) (*TopicData, error) {
	var registered *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		registered, err = d.register(ctx, declared, declaredBy)
		return err
	})
	return registered, err
}

// register registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *TopicDatastore) register(ctx context.Context, declared *TopicData, declaredBy string) (*TopicData, error) {
	// private get, not Get -- otherwise would have nested retries.
	found, err := d.get(ctx, d.Datastore.Pool, declared.Name, declared.SchemaVersion)
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

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `-- vulkan: topic.register
SELECT pg_advisory_xact_lock(hashtext('topic:' || $1));`, declared.Name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.get(ctx, tx, declared.Name, declared.SchemaVersion)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return d.replaceConfig(ctx, found, declared, declaredBy)
	}

	insertSql := `
		-- vulkan: topic.register
		INSERT INTO topic (system_id, name, schema_version, partition_size, retention_ttl_ns, allow_drop_past_committed, idempotency_key_ttl_ns, delivery_log_mode)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at;
	`
	created := *declared
	if err := tx.QueryRow(ctx, insertSql, declared.SystemId, declared.Name, declared.SchemaVersion, declared.PartitionSize, declared.RetentionTTLNs, declared.AllowDropPastCommitted, declared.IdempotencyKeyTTLNs, declared.DeliveryLogMode).
		Scan(&created.Id, &created.CreatedAt, &created.UpdatedAt); err != nil {
		return nil, err
	}

	if err := d.appendTopicLog(ctx, tx, &created, declaredBy); err != nil {
		return nil, err
	}

	if err := d.createTopicTables(ctx, tx, created.Id, declared.PartitionSize); err != nil {
		return nil, err
	}

	// add the migration baseline in the SAME txn
	migrationSql := `
		-- vulkan: topic.register
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

// Rename moves every version under oldName to newName in one transaction,
// appending each version's topic_log row beside the update.
// Returns (nil, nil) if no version is registered under oldName
// ErrTopicNameTaken if newName already has any (name, version) registered.
func (d *TopicDatastore) Rename(ctx context.Context, oldName string, newName string, declaredBy string) ([]TopicData, error) {
	var topics []TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topics, err = d.rename(ctx, oldName, newName, declaredBy)
		return err
	})
	return topics, err
}

func (d *TopicDatastore) rename(ctx context.Context, oldName string, newName string, declaredBy string) ([]TopicData, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	sql := `
		-- vulkan: topic.rename
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
	rows, err := tx.Query(ctx, sql, oldName, newName)
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
			return nil, topic.ErrTopicNameTaken.With("topic", newName)
		}
		return nil, err
	}
	rows.Close()
	if len(topics) == 0 {
		return nil, nil
	}

	for _, data := range topics {
		if err := d.appendTopicLog(ctx, tx, &data, declaredBy); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "topic family renamed", "topic", oldName, "new_name", newName, "version_count", len(topics))
	return topics, nil
}

// appendTopicLog writes data's full snapshot as one topic_log row, inside
// the transaction that changed the topic row.
func (d *TopicDatastore) appendTopicLog(ctx context.Context, q datastore.Querier, data *TopicData, declaredBy string) error {
	sql := `
		-- vulkan: topic.appendTopicLog
		INSERT INTO topic_log (topic_id, name, partition_size, retention_ttl_ns, allow_drop_past_committed, idempotency_key_ttl_ns, delivery_log_mode, declared_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
	`
	_, err := q.Exec(ctx, sql, data.Id, data.Name, data.PartitionSize, data.RetentionTTLNs, data.AllowDropPastCommitted, data.IdempotencyKeyTTLNs, data.DeliveryLogMode, declaredBy)
	return err
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
