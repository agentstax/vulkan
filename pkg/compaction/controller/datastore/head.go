package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// GetCompactionHead reads the current compaction head under compactionKey,
// nil if the key has no head.
func (d *CompactionDatastore) GetCompactionHead(ctx context.Context, topicId int64, compactionKey string) (*MessageData, error) {
	var head *MessageData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		head, err = d.getCompactionHead(ctx, topicId, compactionKey)
		return err
	})
	return head, err
}

func (d *CompactionDatastore) getCompactionHead(ctx context.Context, topicId int64, compactionKey string) (*MessageData, error) {
	sql := fmt.Sprintf(`
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.compaction_key,
			m.compaction_rank
		FROM compaction_head h
		JOIN %s m ON m.id = h.head_id
		WHERE h.topic_id = $1 AND h.compaction_key = $2;
	`, topic.MessageLogTable(topicId))

	var head MessageData
	err := d.Datastore.Pool.QueryRow(ctx, sql, topicId, compactionKey).Scan(
		&head.Id,
		&head.Payload,
		&head.CreatedAt,
		&head.RoutingKey,
		&head.CompactionKey,
		&head.CompactionRank,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &head, nil
}

// ListCompactionHeads reads every key's current head on the topic, ordered by
// compaction key.
func (d *CompactionDatastore) ListCompactionHeads(ctx context.Context, topicId int64) ([]MessageData, error) {
	var heads []MessageData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		heads, err = d.listCompactionHeads(ctx, topicId)
		return err
	})
	return heads, err
}

func (d *CompactionDatastore) listCompactionHeads(ctx context.Context, topicId int64) ([]MessageData, error) {
	sql := fmt.Sprintf(`
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.compaction_key,
			m.compaction_rank
		FROM compaction_head h
		JOIN %s m ON m.id = h.head_id
		WHERE h.topic_id = $1
		ORDER BY h.compaction_key;
	`, topic.MessageLogTable(topicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql, topicId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var heads []MessageData
	for rows.Next() {
		var head MessageData
		if err := rows.Scan(
			&head.Id,
			&head.Payload,
			&head.CreatedAt,
			&head.RoutingKey,
			&head.CompactionKey,
			&head.CompactionRank,
		); err != nil {
			return nil, err
		}
		heads = append(heads, head)
	}
	return heads, rows.Err()
}
