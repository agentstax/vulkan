package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TopicData models the topic table row exactly.
type TopicData struct {
	Id                     int64
	SystemId               int64
	Name                   string
	SchemaVersion          int64
	PartitionSize          int64
	RetentionTTLNs         int64
	AllowDropPastCommitted bool
	IdempotencyKeyTTLNs    int64
	DisableDeliveryLog     bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// AlterTopicData is UpdateTopic's sparse patch -- a nil field means leave
// the column unchanged.
type AlterTopicData struct {
	RetentionTTLNs         *int64
	AllowDropPastCommitted *bool
	IdempotencyKeyTTLNs    *int64
	DisableDeliveryLog     *bool
}

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
			disable_delivery_log,
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
			disable_delivery_log,
			created_at,
			updated_at
		FROM topic
		WHERE id = $1;
	`
	return d.scanTopicData(d.Datastore.Pool.QueryRow(ctx, sql, id))
}

func (d *TopicDatastore) ListTopics(ctx context.Context) ([]*TopicData, error) {
	var topics []*TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topics, err = d.listTopics(ctx)
		return err
	})
	return topics, err
}

func (d *TopicDatastore) listTopics(ctx context.Context) ([]*TopicData, error) {
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
			disable_delivery_log,
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

	var topics []*TopicData
	for rows.Next() {
		topicData, err := d.scanTopicData(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, topicData)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return topics, nil
}

// RegisterTopic resolves data's (name, schema_version) to its row, creating
// it (and its per-topic tables) if it doesn't exist. An existing row must
// match data's tuning columns.
func (d *TopicDatastore) RegisterTopic(ctx context.Context, data *TopicData) (*TopicData, error) {
	var registered *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		registered, err = d.registerTopic(ctx, data)
		return err
	})
	return registered, err
}

// registerTopic registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *TopicDatastore) registerTopic(ctx context.Context, data *TopicData) (*TopicData, error) {
	// private getTopic, not GetTopic -- otherwise would have nested retries.
	found, err := d.getTopic(ctx, d.Datastore.Pool, data.Name, data.SchemaVersion)
	if err != nil {
		return nil, err
	}
	if found != nil {
		if err := d.assertConfigMatches(found, data); err != nil {
			return nil, err
		}
		d.Logger.InfoContext(ctx, "topic registered (already existed)", "topic", found.Name, "topic_id", found.Id, "schema_version", found.SchemaVersion)
		return found, nil
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('topic:' || $1));`, data.Name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.getTopic(ctx, tx, data.Name, data.SchemaVersion)
	if err != nil {
		return nil, err
	}
	if found != nil {
		if err := d.assertConfigMatches(found, data); err != nil {
			return nil, err
		}
		d.Logger.InfoContext(ctx, "topic registered (already existed)", "topic", found.Name, "topic_id", found.Id, "schema_version", found.SchemaVersion)
		return found, nil
	}

	insertSql := `
		INSERT INTO topic (system_id, name, schema_version, partition_size, retention_ttl_ns, allow_drop_past_committed, idempotency_key_ttl_ns, disable_delivery_log)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at;
	`
	created := *data
	if err := tx.QueryRow(ctx, insertSql, data.SystemId, data.Name, data.SchemaVersion, data.PartitionSize, data.RetentionTTLNs, data.AllowDropPastCommitted, data.IdempotencyKeyTTLNs, data.DisableDeliveryLog).
		Scan(&created.Id, &created.CreatedAt, &created.UpdatedAt); err != nil {
		return nil, err
	}

	if err := d.createTopicTables(ctx, tx, created.Id, data.PartitionSize); err != nil {
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

// assertConfigMatches compares the tuning columns -- the db-assigned ones
// (id, timestamps) only exist on found.
func (d *TopicDatastore) assertConfigMatches(found *TopicData, data *TopicData) error {
	matches := found.PartitionSize == data.PartitionSize &&
		found.RetentionTTLNs == data.RetentionTTLNs &&
		found.AllowDropPastCommitted == data.AllowDropPastCommitted &&
		found.IdempotencyKeyTTLNs == data.IdempotencyKeyTTLNs &&
		found.DisableDeliveryLog == data.DisableDeliveryLog
	if !matches {
		return fmt.Errorf("%w: topic %s version %d: existing=%+v got=%+v", topic.ErrTopicConfigMismatch, found.Name, found.SchemaVersion, *found, *data)
	}
	return nil
}

// UpdateTopic applies alter's non-nil fields to (name, schemaVersion).
// Returns (nil, nil) if that (name, schemaVersion) is not found.
func (d *TopicDatastore) UpdateTopic(ctx context.Context, name string, schemaVersion int64, alter *AlterTopicData) (*TopicData, error) {
	var updated *TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		updated, err = d.updateTopic(ctx, name, schemaVersion, alter)
		return err
	})
	return updated, err
}

func (d *TopicDatastore) updateTopic(ctx context.Context, name string, schemaVersion int64, alter *AlterTopicData) (*TopicData, error) {
	// read-before-write is only for the old -> new log line
	old, err := d.getTopic(ctx, d.Datastore.Pool, name, schemaVersion)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, nil
	}

	// a nil param reaches Postgres as NULL
	// COALESCE keeps the column's current value if nil passed
	sql := `
		UPDATE topic
		SET
			retention_ttl_ns = COALESCE($2, retention_ttl_ns),
			allow_drop_past_committed = COALESCE($3, allow_drop_past_committed),
			idempotency_key_ttl_ns = COALESCE($4, idempotency_key_ttl_ns),
			disable_delivery_log = COALESCE($5, disable_delivery_log),
			updated_at = NOW()
		WHERE id = $1
		RETURNING
			id,
			system_id,
			name,
			schema_version,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			disable_delivery_log,
			created_at,
			updated_at;
	`

	row := d.Datastore.Pool.QueryRow(ctx, sql,
		old.Id,
		alter.RetentionTTLNs,
		alter.AllowDropPastCommitted,
		alter.IdempotencyKeyTTLNs,
		alter.DisableDeliveryLog,
	)
	updated, err := d.scanTopicData(row)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		// destroyed between the read and the update
		return nil, nil
	}

	d.Logger.InfoContext(ctx, "topic altered", alterLogFields(old, updated)...)
	return updated, nil
}

// alterLogFields renders old -> new pairs for just the fields that changed.
func alterLogFields(old, updated *TopicData) []any {
	fields := []any{"topic", updated.Name, "topic_id", updated.Id}

	if old.RetentionTTLNs != updated.RetentionTTLNs {
		fields = append(fields, "retention_ttl", fmt.Sprintf("%v -> %v", time.Duration(old.RetentionTTLNs), time.Duration(updated.RetentionTTLNs)))
	}
	if old.AllowDropPastCommitted != updated.AllowDropPastCommitted {
		fields = append(fields, "allow_drop_past_committed", fmt.Sprintf("%v -> %v", old.AllowDropPastCommitted, updated.AllowDropPastCommitted))
	}
	if old.IdempotencyKeyTTLNs != updated.IdempotencyKeyTTLNs {
		fields = append(fields, "idempotency_key_ttl", fmt.Sprintf("%v -> %v", time.Duration(old.IdempotencyKeyTTLNs), time.Duration(updated.IdempotencyKeyTTLNs)))
	}
	if old.DisableDeliveryLog != updated.DisableDeliveryLog {
		fields = append(fields, "disable_delivery_log", fmt.Sprintf("%v -> %v", old.DisableDeliveryLog, updated.DisableDeliveryLog))
	}
	return fields
}

// RenameTopic moves every version under oldName to newName in one statement.
// Returns (nil, nil) if no version is registered under oldName
// ErrTopicNameTaken if newName already has any (name, version) registered.
func (d *TopicDatastore) RenameTopic(ctx context.Context, oldName string, newName string) ([]*TopicData, error) {
	var topics []*TopicData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		topics, err = d.renameTopic(ctx, oldName, newName)
		return err
	})
	return topics, err
}

func (d *TopicDatastore) renameTopic(ctx context.Context, oldName string, newName string) ([]*TopicData, error) {
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
			disable_delivery_log,
			created_at,
			updated_at;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql, oldName, newName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []*TopicData
	for rows.Next() {
		topicData, err := d.scanTopicData(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, topicData)
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
		&data.DisableDeliveryLog,
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
