package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
)

// ListKeyMessages reads messageKey's retained messages, newest first.
func (d *CompactionDatastore) ListKeyMessages(ctx context.Context, topicId int64, messageKey string, limit int) ([]MessageLogRow, error) {
	var messages []MessageLogRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		messages, err = d.listKeyMessages(ctx, topicId, messageKey, limit)
		return err
	})
	return messages, err
}

func (d *CompactionDatastore) listKeyMessages(ctx context.Context, topicId int64, messageKey string, limit int) ([]MessageLogRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: compaction.listKeyMessages
		SELECT
			id,
			payload,
			created_at,
			COALESCE(routing_key, ''),
			message_key,
			COALESCE(compaction_rank, 0)
		FROM %[1]s.%[2]s
		WHERE message_key = $1
		ORDER BY id DESC
		LIMIT $2;
	`, d.Datastore.Schema, topic.MessageLogTable(topicId))

	rows, err := d.Datastore.Pool.Query(ctx, sql, messageKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageLogRow
	for rows.Next() {
		var message MessageLogRow
		if err := rows.Scan(
			&message.Id,
			&message.Payload,
			&message.CreatedAt,
			&message.RoutingKey,
			&message.MessageKey,
			&message.CompactionRank,
		); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
