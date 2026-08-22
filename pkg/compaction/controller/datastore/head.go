package datastore

import (
	"context"
	"errors"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// GetHead reads the current compaction head under compactionKey,
// nil if the key has no head.
func (d *CompactionDatastore) GetHead(ctx context.Context, topicId int64, compactionKey string) (*MessageData, error) {
	var head *MessageData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		head, err = d.getHead(ctx, topicId, compactionKey)
		return err
	})
	return head, err
}

func (d *CompactionDatastore) getHead(ctx context.Context, topicId int64, compactionKey string) (*MessageData, error) {
	sql := fmt.Sprintf(`
		-- vulkan: compaction.getHead
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.compaction_key,
			m.compaction_rank
		FROM %s h
		JOIN %s m ON m.id = h.head_id
		WHERE h.compaction_key = $1;
	`, iTopic.CompactionHeadTable(topicId), iTopic.MessageLogTable(topicId))

	var head MessageData
	err := d.Datastore.Pool.QueryRow(ctx, sql, compactionKey).Scan(
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

// ListHeads reads every key's current head on the topic, ordered by
// compaction key.
func (d *CompactionDatastore) ListHeads(ctx context.Context, topicId int64) ([]MessageData, error) {
	var heads []MessageData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		heads, err = d.listHeads(ctx, topicId)
		return err
	})
	return heads, err
}

func (d *CompactionDatastore) listHeads(ctx context.Context, topicId int64) ([]MessageData, error) {
	sql := fmt.Sprintf(`
		-- vulkan: compaction.listHeads
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.compaction_key,
			m.compaction_rank
		FROM %s h
		JOIN %s m ON m.id = h.head_id
		ORDER BY h.compaction_key;
	`, iTopic.CompactionHeadTable(topicId), iTopic.MessageLogTable(topicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql)
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
