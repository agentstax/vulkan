package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
)

// GetCompactionHead reads the current compaction head under compactionKey on
// its own round trip, nil if the key has no head.
func (d *ProducerDatastore[Message]) GetCompactionHead(ctx context.Context, topicId int64, compactionKey string) (*HeadData, error) {
	var head *HeadData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		head, err = d.getCompactionHead(ctx, d.Datastore.Pool, headSql(topicId, false), topicId, compactionKey)
		return err
	})
	return head, err
}

// GetCompactionHeadInTx reads the head against the caller's tx, locking it
// FOR UPDATE so a following produce on the same key is a race-free
// compare-and-set. No retry: the tx owns its own error handling.
func (d *ProducerDatastore[Message]) GetCompactionHeadInTx(ctx context.Context, tx pgx.Tx, topicId int64, compactionKey string) (*HeadData, error) {
	return d.getCompactionHead(ctx, tx, headSql(topicId, true), topicId, compactionKey)
}

func headSql(topicId int64, forUpdate bool) string {
	lock := ""
	if forUpdate {
		lock = "FOR UPDATE OF h"
	}
	return fmt.Sprintf(`
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, ''),
			m.compaction_key,
			m.compaction_rank
		FROM compaction_head h
		JOIN %s m ON m.id = h.head_id
		WHERE h.topic_id = $1 AND h.compaction_key = $2
		%s;
	`, topic.MessageLogTable(topicId), lock)
}

func (d *ProducerDatastore[Message]) getCompactionHead(ctx context.Context, querier datastore.Querier, sql string, topicId int64, compactionKey string) (*HeadData, error) {
	var head HeadData
	err := querier.QueryRow(ctx, sql, topicId, compactionKey).Scan(
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
