package datastore

import (
	"context"
	"errors"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

// GetCompactionHeadInTx reads the head against the caller's tx, locking it
// FOR UPDATE so a following produce on the same key is a race-free
// compare-and-set. No retry: the tx owns its own error handling.
func (d *ProducerDatastore[Message]) GetCompactionHeadInTx(ctx context.Context, q iDatastore.Querier, topicId int64, compactionKey string) (*HeadData, error) {
	return d.getCompactionHead(ctx, q, topicId, compactionKey)
}

func (d *ProducerDatastore[Message]) getCompactionHead(ctx context.Context, q iDatastore.Querier, topicId int64, compactionKey string) (*HeadData, error) {
	sql := fmt.Sprintf(`
		-- vulkan: producer.getCompactionHead
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.compaction_key,
			m.compaction_rank
		FROM %s h
		JOIN %s m ON m.id = h.head_id
		WHERE h.compaction_key = $1
		FOR UPDATE OF h;
	`, iTopic.CompactionHeadTable(topicId), iTopic.MessageLogTable(topicId))

	var head HeadData
	err := q.QueryRow(ctx, sql, compactionKey).Scan(
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
