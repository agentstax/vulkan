package datastore

import (
	"context"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/topic"
)

// Kill marks expired 'inflight' rows that are out of attempts 'dead' so
// nothing else resolves them. Returns how many rows it marked.
func (d *ExceptionConsumerGroupDatastore) Kill(ctx context.Context, topicId int64, groupId int64, maxRetries int, deliveryLogMode topic.DeliveryLogMode) (int64, error) {
	var killed int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		killed, err = d.kill(ctx, topicId, groupId, maxRetries, deliveryLogMode)
		return err
	})
	return killed, err
}

func (d *ExceptionConsumerGroupDatastore) kill(ctx context.Context, topicId int64, groupId int64, maxRetries int, deliveryLogMode topic.DeliveryLogMode) (int64, error) {
	var killSql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		killSql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.kill
			UPDATE %s
			SET
				status = 'dead',
				lease_token = NULL,
				lease_expires_at = NULL,
				updated_at = now(),
				last_error = concat(last_error, ' [killed: crash-loop hit max attempts]')
			WHERE consumer_group_id = $1
				AND status = 'inflight'
				AND lease_expires_at < now()
				AND attempts - delays >= $2;
		`, iTopic.ExceptionQueueTable(topicId))
	} else {
		// killed CTE + INSERT keeps the kill and its delivery_log_<topic_id> row
		// atomic in one statement.
		killSql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.kill
			WITH killed AS (
				UPDATE %[1]s
				SET
					status = 'dead',
					lease_token = NULL,
					lease_expires_at = NULL,
					updated_at = now(),
					last_error = concat(last_error, ' [killed: crash-loop hit max attempts]')
				WHERE consumer_group_id = $1
					AND status = 'inflight'
					AND lease_expires_at < now()
					AND attempts - delays >= $2
				RETURNING consumer_group_id, message_id, attempts, last_error
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT consumer_group_id, message_id, attempts, 'killed', last_error
			FROM killed;
		`, iTopic.ExceptionQueueTable(topicId), iTopic.DeliveryLogTable(topicId))
	}
	killTag, err := d.Datastore.Pool.Exec(ctx, killSql, groupId, maxRetries)
	if err != nil {
		return 0, err
	}
	if killTag.RowsAffected() > 0 {
		d.Logger.WarnContext(ctx, consumergroup.EventKillBackstopFired.Message, "code", consumergroup.EventKillBackstopFired.Code, "group_id", groupId, "topic_id", topicId, "dead_count", killTag.RowsAffected())
	}
	return killTag.RowsAffected(), nil
}
