package datastore

import (
	"context"
	"fmt"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// ClaimMessagesWithCursor tries to pick up a crashed range (an expired lease)
// and only claims fresh work from the frontier if there's nothing to reclaim --
// so crashed ranges drain first.
func (d *MessageConsumerGroupDatastore) ClaimMessagesWithCursor(ctx context.Context, topicId int64, groupId int64, limit int, maxRangeReclaims int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) (*ClaimedRangeData, error) {
	var claimed *ClaimedRangeData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimMessagesWithCursor(ctx, topicId, groupId, limit, maxRangeReclaims, leaseDuration, deliveryLogMode)
		return err
	})
	return claimed, err
}

func (d *MessageConsumerGroupDatastore) claimMessagesWithCursor(ctx context.Context, topicId int64, groupId int64, limit int, maxRangeReclaims int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) (*ClaimedRangeData, error) {
	reclaimed, err := d.reclaimWithCursor(ctx, topicId, groupId, maxRangeReclaims, leaseDuration, deliveryLogMode)
	if err != nil {
		return nil, err
	}
	if reclaimed != nil {
		return reclaimed, nil
	}

	// nothing to reclaim -> try standard fresh claim (nil when caught up)
	return d.freshClaimMessagesWithCursor(ctx, topicId, groupId, limit, leaseDuration)
}

// readMessages reads topicId's message_log rows in (low, high], ordered by id.
func (d *MessageConsumerGroupDatastore) readMessages(ctx context.Context, tx pgx.Tx, topicId int64, groupId int64, low int64, high int64) ([]MessageData, error) {
	sql := fmt.Sprintf(`
		-- vulkan: messageconsumer.readMessages
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
					SELECT 1 FROM %s b
					WHERE b.consumer_group_id = $3
				)
				-- bindings for consumer_group exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM %s b
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
					SELECT head_id FROM %s
					WHERE compaction_key = m.compaction_key
				)
			)
		-- rows MUST come back in id order or a batch LIMIT could
		-- return an arbitrary subset and the cursor would advance past unread offsets
		ORDER BY m.id;
	`, iTopic.MessageLogTable(topicId), iTopic.BindingTable(topicId), iTopic.BindingTable(topicId), iTopic.CompactionHeadTable(topicId))

	rows, err := tx.Query(ctx, sql, low, high, groupId)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[MessageData])
}
