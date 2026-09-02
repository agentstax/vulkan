package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RecordSuccess marks a claimed delivery 'done'; DeliveryLogModeAll also writes
// the 'success' log row in the same statement. Terminal success for this
// (group, message); the message row is untouched and other groups are
// unaffected.
func (d *DeliveryConsumerGroupDatastore) RecordSuccess(ctx context.Context, delivery *ExceptionQueueRow, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordSuccess(ctx, delivery, deliveryLogMode)
	})
}

func (d *DeliveryConsumerGroupDatastore) recordSuccess(ctx context.Context, delivery *ExceptionQueueRow, deliveryLogMode topic.DeliveryLogMode) error {
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeAll {
		// updated CTE + INSERT keeps the 'done' mark and its
		// delivery_log_<topic_id> row atomic
		sql = fmt.Sprintf(`
			-- vulkan: deliveryconsumer.recordSuccess
			WITH updated AS (
				UPDATE %[1]s.%[2]s
				SET
					status = 'done',
					last_error = NULL,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
				RETURNING attempts
			)
			INSERT INTO %[1]s.%[3]s (consumer_group_id, message_id, attempt, status, error)
			SELECT $1, $2, attempts, 'success', ''
			FROM updated;
		`, d.Datastore.Schema, topic.ExceptionQueueTable(delivery.TopicId), topic.DeliveryLogTable(delivery.TopicId))
	} else {
		sql = fmt.Sprintf(`
			-- vulkan: deliveryconsumer.recordSuccess
			UPDATE %[1]s.%[2]s
			SET
				status = 'done',
				last_error = NULL,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, d.Datastore.Schema, topic.ExceptionQueueTable(delivery.TopicId))
	}

	_, err := d.Datastore.Pool.Exec(ctx, sql, delivery.ConsumerGroupId, delivery.MessageId)
	return err
}

// RecordFailure handles a processing error: retry until attempts are exhausted,
// then hand off to RecordTerminal (the per-group DLQ). attempts was already
// incremented at claim time, so >= maxAttempts means this was the last try.
// No retry backoff (the exception_queue table carries no can_run_after) -- a
// 'ready' row is simply re-claimed on the next poll.
func (d *DeliveryConsumerGroupDatastore) RecordFailure(ctx context.Context, maxAttempts int, delivery *ExceptionQueueRow, failureErr error, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordFailure(ctx, maxAttempts, delivery, failureErr, deliveryLogMode)
	})
}

func (d *DeliveryConsumerGroupDatastore) recordFailure(ctx context.Context, maxAttempts int, delivery *ExceptionQueueRow, failureErr error, deliveryLogMode topic.DeliveryLogMode) error {
	if delivery.Attempts >= maxAttempts {
		// private call, not the exported RecordTerminal -- this already runs
		// inside RecordFailure's own Retry.Wrap, calling the exported one
		// would nest a second retry loop around the same round-trip.
		return d.recordTerminal(ctx, delivery, failureErr, deliveryLogMode)
	}

	var sql string
	args := []any{delivery.ConsumerGroupId, delivery.MessageId, failureErr.Error()}
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			-- vulkan: deliveryconsumer.recordFailure
			UPDATE %[1]s.%[2]s
			SET
				status = 'ready',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, d.Datastore.Schema, topic.ExceptionQueueTable(delivery.TopicId))
	} else {
		sql = fmt.Sprintf(`
			-- vulkan: deliveryconsumer.recordFailure
			WITH updated AS (
				UPDATE %[1]s.%[2]s
				SET
					status = 'ready',
					last_error = $3,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
				RETURNING 1
			)
			INSERT INTO %[1]s.%[3]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, $2, $4, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, d.Datastore.Schema, topic.ExceptionQueueTable(delivery.TopicId), topic.DeliveryLogTable(delivery.TopicId))
		args = append(args, delivery.Attempts)
	}

	_, err := d.Datastore.Pool.Exec(ctx, sql, args...)
	return err
}

// RecordTerminal dead-letters a delivery: no more retries. The DLQ for a group is
// just `WHERE consumer_group_id = $1 AND status = 'dead'`; one group can dead-letter a
// message while another processes the same offset fine.
func (d *DeliveryConsumerGroupDatastore) RecordTerminal(ctx context.Context, delivery *ExceptionQueueRow, terminalErr error, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordTerminal(ctx, delivery, terminalErr, deliveryLogMode)
	})
}

func (d *DeliveryConsumerGroupDatastore) recordTerminal(ctx context.Context, delivery *ExceptionQueueRow, terminalErr error, deliveryLogMode topic.DeliveryLogMode) error {
	var sql string
	args := []any{delivery.ConsumerGroupId, delivery.MessageId, terminalErr.Error()}
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			-- vulkan: deliveryconsumer.recordTerminal
			UPDATE %[1]s.%[2]s
			SET
				status = 'dead',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, d.Datastore.Schema, topic.ExceptionQueueTable(delivery.TopicId))
	} else {
		sql = fmt.Sprintf(`
			-- vulkan: deliveryconsumer.recordTerminal
			WITH updated AS (
				UPDATE %[1]s.%[2]s
				SET
					status = 'dead',
					last_error = $3,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
				RETURNING 1
			)
			INSERT INTO %[1]s.%[3]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, $2, $4, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, d.Datastore.Schema, topic.ExceptionQueueTable(delivery.TopicId), topic.DeliveryLogTable(delivery.TopicId))
		args = append(args, delivery.Attempts)
	}

	if _, err := d.Datastore.Pool.Exec(ctx, sql, args...); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, consumergroup.EventMessageDeadLettered.Message, "code", consumergroup.EventMessageDeadLettered.Code, "group_id", delivery.ConsumerGroupId, "topic_id", delivery.TopicId, "message_id", delivery.MessageId, "error", terminalErr)
	return nil
}
