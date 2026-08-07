package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/internal/topic"
)

// RecordSuccess marks a claimed delivery 'done'. Terminal success for this
// (group, message); the log row is untouched and other groups are unaffected.
func (d *DeliveryConsumerDatastore) RecordSuccess(ctx context.Context, delivery *DeliveryData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordSuccess(ctx, delivery)
	})
}

func (d *DeliveryConsumerDatastore) recordSuccess(ctx context.Context, delivery *DeliveryData) error {
	sql := fmt.Sprintf(`
		UPDATE %s
		SET
			status = 'done',
			last_error = NULL,
			updated_at = now()
		WHERE consumer_group_id = $1
			AND message_id = $2;
	`, topic.DeliveryTable(delivery.TopicID))

	_, err := d.Datastore.Pool.Exec(ctx, sql, delivery.ConsumerGroupId, delivery.MessageId)
	return err
}

// RecordFailure handles a processing error: retry until attempts are exhausted,
// then hand off to RecordTerminal (the per-group DLQ). attempts was already
// incremented at claim time, so >= maxAttempts means this was the last try.
// No retry backoff (the delivery table carries no can_run_after) -- a
// 'ready' row is simply re-claimed on the next poll.
func (d *DeliveryConsumerDatastore) RecordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryData, failureErr error, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordFailure(ctx, maxAttempts, delivery, failureErr, disableDeliveryLog)
	})
}

func (d *DeliveryConsumerDatastore) recordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryData, failureErr error, disableDeliveryLog bool) error {
	if delivery.Attempts >= maxAttempts {
		// private call, not the exported RecordTerminal -- this already runs
		// inside RecordFailure's own Retry.Wrap, calling the exported one
		// would nest a second retry loop around the same round-trip.
		return d.recordTerminal(ctx, delivery, failureErr, disableDeliveryLog)
	}

	var sql string
	args := []any{delivery.ConsumerGroupId, delivery.MessageId, failureErr.Error()}
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'ready',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, topic.DeliveryTable(delivery.TopicID))
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
		`, topic.DeliveryTable(delivery.TopicID), topic.DeliveryLogTable(delivery.TopicID))
		args = append(args, delivery.Attempts)
	}

	_, err := d.Datastore.Pool.Exec(ctx, sql, args...)
	return err
}

// RecordTerminal dead-letters a delivery: no more retries. The DLQ for a group is
// just `WHERE consumer_group_id = $1 AND status = 'dead'`; one group can dead-letter a
// message while another processes the same offset fine.
func (d *DeliveryConsumerDatastore) RecordTerminal(ctx context.Context, delivery *DeliveryData, terminalErr error, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordTerminal(ctx, delivery, terminalErr, disableDeliveryLog)
	})
}

func (d *DeliveryConsumerDatastore) recordTerminal(ctx context.Context, delivery *DeliveryData, terminalErr error, disableDeliveryLog bool) error {
	var sql string
	args := []any{delivery.ConsumerGroupId, delivery.MessageId, terminalErr.Error()}
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'dead',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2;
		`, topic.DeliveryTable(delivery.TopicID))
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
		`, topic.DeliveryTable(delivery.TopicID), topic.DeliveryLogTable(delivery.TopicID))
		args = append(args, delivery.Attempts)
	}

	if _, err := d.Datastore.Pool.Exec(ctx, sql, args...); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, "message dead-lettered", "group_id", delivery.ConsumerGroupId, "topic_id", delivery.TopicID, "message_id", delivery.MessageId, "error", terminalErr)
	return nil
}
