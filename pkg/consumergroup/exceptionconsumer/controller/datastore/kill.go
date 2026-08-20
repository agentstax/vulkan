package datastore

import (
	"context"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/topic"
)

// Kill marks expired 'inflight' rows that are out of
// attempts 'dead' so nothing else resolves them.
func (d *ExceptionConsumerGroupDatastore) Kill(ctx context.Context, topicId int64, groupId int64, maxRetries int, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.kill(ctx, topicId, groupId, maxRetries, deliveryLogMode)
	})
}

func (d *ExceptionConsumerGroupDatastore) kill(ctx context.Context, topicId int64, groupId int64, maxRetries int, deliveryLogMode topic.DeliveryLogMode) error {
	var killSql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		killSql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'dead',
				lease_token = NULL,
				lease_until = NULL,
				updated_at = now(),
				last_error = concat(last_error, ' [killed: crash-loop hit max attempts]')
			WHERE consumer_group_id = $1
				AND status = 'inflight'
				AND lease_until < now()
				AND attempts >= $2;
		`, iTopic.DeliveryTable(topicId))
	} else {
		// killed CTE + INSERT keeps the kill and its delivery_log_<topic_id> row
		// atomic in one statement.
		killSql = fmt.Sprintf(`
			WITH killed AS (
				UPDATE %[1]s
				SET
					status = 'dead',
					lease_token = NULL,
					lease_until = NULL,
					updated_at = now(),
					last_error = concat(last_error, ' [killed: crash-loop hit max attempts]')
				WHERE consumer_group_id = $1
					AND status = 'inflight'
					AND lease_until < now()
					AND attempts >= $2
				RETURNING consumer_group_id, message_id, attempts, last_error
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT consumer_group_id, message_id, attempts, 'killed', last_error
			FROM killed;
		`, iTopic.DeliveryTable(topicId), iTopic.DeliveryLogTable(topicId))
	}
	killTag, err := d.Datastore.Pool.Exec(ctx, killSql, groupId, maxRetries)
	if err != nil {
		return err
	}
	if killTag.RowsAffected() > 0 {
		d.Logger.WarnContext(ctx, "crash-loop kill backstop fired, exception(s) marked dead", "group_id", groupId, "topic_id", topicId, "count", killTag.RowsAffected())
	}
	return nil
}
