package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// ClaimMessagesWithCursor tries to pick up a crashed range (an expired lease)
// and only claims fresh work from the frontier if there's nothing to reclaim --
// so crashed ranges drain first.
func (d *MessageConsumerDatastore) ClaimMessagesWithCursor(ctx context.Context, topicID int64, groupID int64, limit int, maxRangeReclaims int, leaseDuration time.Duration, disableDeliveryLog bool) (*ClaimedRangeData, error) {
	var claimed *ClaimedRangeData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimMessagesWithCursor(ctx, topicID, groupID, limit, maxRangeReclaims, leaseDuration, disableDeliveryLog)
		return err
	})
	return claimed, err
}

func (d *MessageConsumerDatastore) claimMessagesWithCursor(ctx context.Context, topicID int64, groupID int64, limit int, maxRangeReclaims int, leaseDuration time.Duration, disableDeliveryLog bool) (*ClaimedRangeData, error) {
	reclaimed, err := d.reclaimWithCursor(ctx, topicID, groupID, maxRangeReclaims, leaseDuration, disableDeliveryLog)
	if err != nil {
		return nil, err
	}
	if reclaimed != nil {
		return reclaimed, nil
	}

	// nothing to reclaim, or the one reclaimable range was poisoned and just got
	// quarantined instead -> try standard fresh claim (nil when caught up)
	return d.freshClaimMessagesWithCursor(ctx, topicID, groupID, limit, leaseDuration)
}

// readMessages reads topicID's message_log rows in (low, high], ordered by id.
func (d *MessageConsumerDatastore) readMessages(ctx context.Context, tx pgx.Tx, topicID int64, groupID int64, low int64, high int64) ([]MessageData, error) {
	sql := fmt.Sprintf(`
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, '') AS routing_key,
			COALESCE(m.compaction_key, '') AS compaction_key,
			m.compaction_rank,
			m.options
		FROM %s m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				-- no bindings for consumer_group exists
				NOT EXISTS (
					SELECT 1 FROM binding b
					WHERE b.consumer_group_id = $3
				)
				-- bindings for consumer_group exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM binding b
					WHERE b.consumer_group_id = $3
						AND m.routing_key ~ b.pattern
				)
				-- if bindings exist but our routing_key does not match any of them
				-- we do not return anything
			)
			AND (
				-- unkeyed rows are never compacted
				m.compaction_key IS NULL
				-- keyed rows are eligible only if they're compaction_head's current
				-- pointer for their key -- O(1) lookup, no per-row scan
				OR m.id = (
					SELECT head_id FROM compaction_head
					WHERE topic_id = $4
						AND compaction_key = m.compaction_key
				)
			)
		-- rows MUST come back in id order or a batch LIMIT could
		-- return an arbitrary subset and the cursor would advance past unread offsets
		ORDER BY m.id;
	`, topic.MessageLogTable(topicID))

	rows, err := tx.Query(ctx, sql, low, high, groupID, topicID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageData])
}
