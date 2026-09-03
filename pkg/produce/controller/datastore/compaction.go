package datastore

import (
	"context"
	"errors"
	"fmt"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// GetCompactionHeadInTx reads the head against the caller's tx, locking it
// FOR UPDATE so a following produce on the same key is a race-free
// compare-and-set. No retry: the tx owns its own error handling.
func (d *ProduceDatastore) GetCompactionHeadInTx(ctx context.Context, q iDatastore.Querier, topicId int64, messageKey string) (*MessageLogRow, error) {
	return d.getCompactionHead(ctx, q, topicId, messageKey)
}

func (d *ProduceDatastore) getCompactionHead(ctx context.Context, q iDatastore.Querier, topicId int64, messageKey string) (*MessageLogRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: produce.getCompactionHead
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.message_key,
			m.compaction_rank
		FROM %[1]s.%[2]s h
		JOIN %[1]s.%[3]s m ON m.id = h.head_id
		WHERE h.compaction_key = $1
		FOR UPDATE OF h;
	`, d.Datastore.Schema, topic.CompactionHeadTable(topicId), topic.MessageLogTable(topicId))

	var head MessageLogRow
	err := q.QueryRow(ctx, sql, messageKey).Scan(
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
