package datastore

import (
	"context"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RecordSuccess marks a claimed delivery 'done'; DeliveryLogModeAll also writes
// the 'success' log row in the same statement. Terminal success for this
// (group, message); the message row is untouched and other groups are
// unaffected.
func (d *DeliveryConsumerDatastore) RecordSuccess(ctx context.Context, delivery *DeliveryData, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordSuccess(ctx, delivery, deliveryLogMode)
	})
}

func (d *DeliveryConsumerDatastore) recordSuccess(ctx context.Context, delivery *DeliveryData, deliveryLogMode topic.DeliveryLogMode) error {
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeAll {
		// updated CTE + INSERT keeps the 'done' mark and its
		// delivery_log_<topic_id> row atomic
		sql = fmt.Sprintf(`
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'done',
					last_error = NULL,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
				RETURNING attempts
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT $1, $2, attempts, 'success', ''
			FROM updated;
		`, iTopic.DeliveryTable(delivery.TopicId), iTopic.DeliveryLogTable(delivery.TopicId))
	} else {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'done',
				last_error = NULL,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, iTopic.DeliveryTable(delivery.TopicId))
	}

	_, err := d.Datastore.Pool.Exec(ctx, sql, delivery.ConsumerGroupId, delivery.MessageId)
	return err
}

// RecordFailure handles a processing error: retry until attempts are exhausted,
// then hand off to RecordTerminal (the per-group DLQ). attempts was already
// incremented at claim time, so >= maxAttempts means this was the last try.
// No retry backoff (the delivery table carries no can_run_after) -- a
// 'ready' row is simply re-claimed on the next poll.
func (d *DeliveryConsumerDatastore) RecordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryData, failureErr error, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordFailure(ctx, maxAttempts, delivery, failureErr, deliveryLogMode)
	})
}

func (d *DeliveryConsumerDatastore) recordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryData, failureErr error, deliveryLogMode topic.DeliveryLogMode) error {
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
			UPDATE %s
			SET
				status = 'ready',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, iTopic.DeliveryTable(delivery.TopicId))
	} else {
		sql = fmt.Sprintf(`
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'ready',
					last_error = $3,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, $2, $4, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, iTopic.DeliveryTable(delivery.TopicId), iTopic.DeliveryLogTable(delivery.TopicId))
		args = append(args, delivery.Attempts)
	}

	_, err := d.Datastore.Pool.Exec(ctx, sql, args...)
	return err
}

// RecordTerminal dead-letters a delivery: no more retries. The DLQ for a group is
// just `WHERE consumer_group_id = $1 AND status = 'dead'`; one group can dead-letter a
// message while another processes the same offset fine.
func (d *DeliveryConsumerDatastore) RecordTerminal(ctx context.Context, delivery *DeliveryData, terminalErr error, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordTerminal(ctx, delivery, terminalErr, deliveryLogMode)
	})
}

func (d *DeliveryConsumerDatastore) recordTerminal(ctx context.Context, delivery *DeliveryData, terminalErr error, deliveryLogMode topic.DeliveryLogMode) error {
	var sql string
	args := []any{delivery.ConsumerGroupId, delivery.MessageId, terminalErr.Error()}
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'dead',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, iTopic.DeliveryTable(delivery.TopicId))
	} else {
		sql = fmt.Sprintf(`
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'dead',
					last_error = $3,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, $2, $4, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, iTopic.DeliveryTable(delivery.TopicId), iTopic.DeliveryLogTable(delivery.TopicId))
		args = append(args, delivery.Attempts)
	}

	if _, err := d.Datastore.Pool.Exec(ctx, sql, args...); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, "message dead-lettered", "group_id", delivery.ConsumerGroupId, "topic_id", delivery.TopicId, "message_id", delivery.MessageId, "error", terminalErr)
	return nil
}
