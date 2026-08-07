package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

func (d *DeliveryConsumerDatastore) ClaimMessagesWithLifecycle(ctx context.Context, topicID int64, groupID int64, limit int) ([]DeliveryData, error) {
	var deliveries []DeliveryData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		deliveries, err = d.claimMessagesWithLifecycle(ctx, topicID, groupID, limit)
		return err
	})
	return deliveries, err
}

func (d *DeliveryConsumerDatastore) claimMessagesWithLifecycle(ctx context.Context, topicID int64, groupID int64, limit int) ([]DeliveryData, error) {
	// Claim this group's own delivery rows and move them 'ready' -> 'processing' in
	// one statement, per (group, topic, message). SKIP LOCKED keeps competing
	// workers from grabbing the same row.
	//
	// delivery only stores message_id, not the payload, so we join this topic's
	// message_log back in -- the log stays immutable, all mutation lives in delivery.
	//
	// No lease here: the parked lifecycle path never grew crash recovery, so a
	// 'processing' row that never gets resolved (consumer crash) just sits there.
	sql := fmt.Sprintf(`
		WITH claimed AS (
			UPDATE %[1]s
			SET
				status = 'processing',
				attempts = attempts + 1,
				updated_at = now()
			WHERE (consumer_group_id, message_id) IN (
				SELECT consumer_group_id, message_id FROM %[1]s
				WHERE consumer_group_id = $1
					AND status = 'ready'
				ORDER BY message_id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING consumer_group_id, message_id, status, attempts
		)
		SELECT
			c.consumer_group_id,
			$3::bigint AS topic_id,
			c.message_id,
			c.status,
			c.attempts,
			m.payload,
			m.options
		FROM claimed c
		JOIN %[2]s m ON m.id = c.message_id
		ORDER BY c.message_id;
	`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, sql, groupID, limit, topicID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[DeliveryData])
}
