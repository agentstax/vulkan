package topic

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

type topicDatastore struct {
	Datastore *datastore.PostgresDatastore
}

func newTopicDatastore(datastore *datastore.PostgresDatastore) *topicDatastore {
	return &topicDatastore{
		Datastore: datastore,
	}
}

func (d *topicDatastore) GetTopic(ctx context.Context, name string) (*Topic, error) {
	sql := `
		SELECT id, name, partition_size FROM topics WHERE name = $1;
	`

	var t Topic
	err := d.Datastore.Pool.QueryRow(ctx, sql, name).Scan(
		&t.Id,
		&t.Name,
		&t.PartitionSize,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &t, nil
}

// UpsertTopic resolves cfg.Name to its db identity, creating it if it doesn't exist.
func (d *topicDatastore) UpsertTopic(ctx context.Context, cfg Config) (*Topic, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	insertSql := `
		INSERT INTO topics (name, partition_size)
		VALUES ($1, $2)
		ON CONFLICT (name) DO NOTHING -- no rows are returned on conflict -> must GetTopic later
		RETURNING id;
	`

	var id int64
	insertErr := tx.QueryRow(ctx, insertSql, cfg.Name, cfg.PartitionSize).Scan(&id)

	switch {
	case insertErr == nil:
		// we won the insert -- stand up this topic's own log
		if err := d.createTopicLog(ctx, tx, id, cfg.PartitionSize); err != nil {
			return nil, err
		}
	case errors.Is(insertErr, pgx.ErrNoRows):
		// not writing here, so reading via the plain pool (not tx) is fine
		found, err := d.GetTopic(ctx, cfg.Name)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, fmt.Errorf("topic %s: lost the registration race and could not be resolved", cfg.Name)
		}
		id = found.Id
		if want := cfg.ToTopic(id); *found != *want {
			return nil, fmt.Errorf("%w: topic %s: existing=%+v got=%+v", ErrTopicConfigMismatch, cfg.Name, *found, *want)
		}
	default:
		return nil, insertErr
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return cfg.ToTopic(id), nil
}

// createTopicLog creates this topic's own message_log_<id>, plus its first partition.
func (d *topicDatastore) createTopicLog(ctx context.Context, tx pgx.Tx, id int64, partitionSize int64) error {
	createTableSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS message_log_%d (
			id BIGSERIAL PRIMARY KEY, -- own sequence per table, so each topic's ids are independent
			routing_key TEXT,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		) PARTITION BY RANGE (id);
	`, id)
	if _, err := tx.Exec(ctx, createTableSql); err != nil {
		return err
	}

	// message_log_<id>_<n> -- two-part name avoids colliding with another topic's table
	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS message_log_%d_0
			PARTITION OF message_log_%d
			FOR VALUES FROM (0) TO (%d);
	`, id, id, partitionSize)
	_, err := tx.Exec(ctx, createPartitionSql)
	return err
}

func (d *topicDatastore) DeleteTopic(ctx context.Context, topic *Topic) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM topics WHERE id = $1;`, topic.Id); err != nil {
		return err
	}

	// drops every message_log_<id>_N partition with it
	dropTableSql := fmt.Sprintf(`DROP TABLE IF EXISTS message_log_%d;`, topic.Id)
	if _, err := tx.Exec(ctx, dropTableSql); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
