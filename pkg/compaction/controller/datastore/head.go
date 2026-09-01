package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// GetHead reads the current compaction head under messageKey,
// nil if the key has no head.
func (d *CompactionDatastore) GetHead(ctx context.Context, topicId int64, messageKey string) (*MessageLogRow, error) {
	var head *MessageLogRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		head, err = d.getHead(ctx, topicId, messageKey)
		return err
	})
	return head, err
}

func (d *CompactionDatastore) getHead(ctx context.Context, topicId int64, messageKey string) (*MessageLogRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: compaction.getHead
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.message_key,
			m.compaction_rank
		FROM %s h
		JOIN %s m ON m.id = h.head_id
		WHERE h.compaction_key = $1;
	`, topic.CompactionHeadTable(topicId), topic.MessageLogTable(topicId))

	var head MessageLogRow
	err := d.Datastore.Pool.QueryRow(ctx, sql, messageKey).Scan(
		&head.Id,
		&head.Payload,
		&head.CreatedAt,
		&head.RoutingKey,
		&head.MessageKey,
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
// message key.
func (d *CompactionDatastore) ListHeads(ctx context.Context, topicId int64) ([]MessageLogRow, error) {
	var heads []MessageLogRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		heads, err = d.listHeads(ctx, topicId)
		return err
	})
	return heads, err
}

func (d *CompactionDatastore) listHeads(ctx context.Context, topicId int64) ([]MessageLogRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: compaction.listHeads
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.message_key,
			m.compaction_rank
		FROM %s h
		JOIN %s m ON m.id = h.head_id
		ORDER BY h.compaction_key;
	`, topic.CompactionHeadTable(topicId), topic.MessageLogTable(topicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var heads []MessageLogRow
	for rows.Next() {
		var head MessageLogRow
		if err := rows.Scan(
			&head.Id,
			&head.Payload,
			&head.CreatedAt,
			&head.RoutingKey,
			&head.MessageKey,
			&head.CompactionRank,
		); err != nil {
			return nil, err
		}
		heads = append(heads, head)
	}
	return heads, rows.Err()
}
