package datastore

import (
	"context"
	"fmt"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/topic"
)

// RecordSuccess deletes the row.
// DeliveryLogModeAll also writes the 'success' log row in the same statement.
// A non-nil keyClaim frees the key in the same transaction.
func (d *ExceptionConsumerGroupDatastore) RecordSuccess(ctx context.Context, exception *ExceptionData, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordSuccess(ctx, exception, deliveryLogMode, keyClaim)
	})
}

func (d *ExceptionConsumerGroupDatastore) recordSuccess(ctx context.Context, exception *ExceptionData, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeAll {
		// deleted CTE + INSERT keeps the success-deletion and its
		// delivery_log_<topic_id> row atomic
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordSuccess
			WITH deleted AS (
				DELETE FROM %[1]s
				WHERE consumer_group_id = $1
					AND message_id = $2
					AND lease_token = $3
				RETURNING attempts
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT $1, $2, attempts, 'success', ''
			FROM deleted;
		`, iTopic.ExceptionQueueTable(exception.TopicId), iTopic.DeliveryLogTable(exception.TopicId))
	} else {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordSuccess
			DELETE FROM %s
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $3;
		`, iTopic.ExceptionQueueTable(exception.TopicId))
	}

	if keyClaim == nil {
		return d.record(ctx, sql, exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken)
	}
	return d.recordAndReleaseKey(ctx, keyClaim, sql, exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken)
}

// RecordFailure resets the row 'ready' with retryPolicy's backoff so it can
// be retried. Exhausted attempts are the caller's call -- it records those
// through RecordTerminal instead.
// A non-nil keyClaim frees the key in the same transaction.
func (d *ExceptionConsumerGroupDatastore) RecordFailure(ctx context.Context, retryPolicy *common.RetryPolicy, exception *ExceptionData, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordFailure(ctx, retryPolicy, exception, failureErr, deliveryLogMode, keyClaim)
	})
}

func (d *ExceptionConsumerGroupDatastore) recordFailure(ctx context.Context, retryPolicy *common.RetryPolicy, exception *ExceptionData, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	// clears the lease so it's claimable as a fresh 'ready' retry once can_run_after passes.
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordFailure
			UPDATE %s
			SET
				status = 'ready',
				lease_token = NULL,
				lease_expires_at = NULL,
				last_error = $3,
				can_run_after = now() + make_interval(secs => $4),
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $5;
		`, iTopic.ExceptionQueueTable(exception.TopicId))
	} else {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordFailure
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'ready',
					lease_token = NULL,
					lease_expires_at = NULL,
					last_error = $3,
					can_run_after = now() + make_interval(secs => $4),
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
					AND lease_token = $5
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, $2, $6, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, iTopic.ExceptionQueueTable(exception.TopicId), iTopic.DeliveryLogTable(exception.TopicId))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, failureErr.Error(), retryPolicy.CalculateDelay(exception.Attempts - exception.Delays - 1).Seconds(), exception.LeaseToken}
	if deliveryLogMode != topic.DeliveryLogModeOff {
		args = append(args, exception.Attempts)
	}

	if keyClaim == nil {
		return d.record(ctx, sql, args...)
	}
	return d.recordAndReleaseKey(ctx, keyClaim, sql, args...)
}

// RecordDelayed resets the row 'ready' at the handler's requested delay and
// counts it in delays, not as a failure. The delivery_log row lands at this
// attempt with status 'delayed'.
// A non-nil keyClaim frees the key in the same transaction.
func (d *ExceptionConsumerGroupDatastore) RecordDelayed(ctx context.Context, delay time.Duration, exception *ExceptionData, delayErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordDelayed(ctx, delay, exception, delayErr, deliveryLogMode, keyClaim)
	})
}

func (d *ExceptionConsumerGroupDatastore) recordDelayed(ctx context.Context, delay time.Duration, exception *ExceptionData, delayErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordDelayed
			UPDATE %s
			SET
				status = 'ready',
				delays = delays + 1,
				lease_token = NULL,
				lease_expires_at = NULL,
				last_error = $3,
				can_run_after = now() + make_interval(secs => $4),
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $5;
		`, iTopic.ExceptionQueueTable(exception.TopicId))
	} else {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordDelayed
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'ready',
					delays = delays + 1,
					lease_token = NULL,
					lease_expires_at = NULL,
					last_error = $3,
					can_run_after = now() + make_interval(secs => $4),
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
					AND lease_token = $5
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT $1, $2, $6, 'delayed', $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, iTopic.ExceptionQueueTable(exception.TopicId), iTopic.DeliveryLogTable(exception.TopicId))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, delayErr.Error(), delay.Seconds(), exception.LeaseToken}
	if deliveryLogMode != topic.DeliveryLogModeOff {
		args = append(args, exception.Attempts)
	}

	if keyClaim == nil {
		return d.record(ctx, sql, args...)
	}
	return d.recordAndReleaseKey(ctx, keyClaim, sql, args...)
}

// RecordTerminal marks the row 'dead' -- no retry could succeed.
// A non-nil keyClaim frees the key in the same transaction.
func (d *ExceptionConsumerGroupDatastore) RecordTerminal(ctx context.Context, exception *ExceptionData, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordTerminal(ctx, exception, failureErr, deliveryLogMode, keyClaim)
	})
}

func (d *ExceptionConsumerGroupDatastore) recordTerminal(ctx context.Context, exception *ExceptionData, failureErr error, deliveryLogMode topic.DeliveryLogMode, keyClaim *KeyLeaseData) error {
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordTerminal
			UPDATE %s
			SET
				status = 'dead',
				lease_token = NULL,
				lease_expires_at = NULL,
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $4;
		`, iTopic.ExceptionQueueTable(exception.TopicId))
	} else {
		// updated CTE + INSERT ... WHERE EXISTS keeps the UPDATE and its
		// delivery_log_<topic_id> row atomic
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordTerminal
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'dead',
					lease_token = NULL,
					lease_expires_at = NULL,
					last_error = $3,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
					AND lease_token = $4
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, $2, $5, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, iTopic.ExceptionQueueTable(exception.TopicId), iTopic.DeliveryLogTable(exception.TopicId))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, failureErr.Error(), exception.LeaseToken}
	if deliveryLogMode != topic.DeliveryLogModeOff {
		args = append(args, exception.Attempts)
	}

	if keyClaim == nil {
		if err := d.record(ctx, sql, args...); err != nil {
			return err
		}
	} else {
		if err := d.recordAndReleaseKey(ctx, keyClaim, sql, args...); err != nil {
			return err
		}
	}

	d.Logger.WarnContext(ctx, consumergroup.EventExceptionDeadLettered.Message, "code", consumergroup.EventExceptionDeadLettered.Code, "group_id", exception.ConsumerGroupId, "topic_id", exception.TopicId, "message_id", exception.MessageId, "attempts", exception.Attempts)
	return nil
}

// RecordSuperseded never runs the row again: the claim's attempts
// increment is decremented back and the log row lands at that attempt.
func (d *ExceptionConsumerGroupDatastore) RecordSuperseded(ctx context.Context, exception *ExceptionData, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordSuperseded(ctx, exception, deliveryLogMode)
	})
}

func (d *ExceptionConsumerGroupDatastore) recordSuperseded(ctx context.Context, exception *ExceptionData, deliveryLogMode topic.DeliveryLogMode) error {
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordSuperseded
			UPDATE %s
			SET
				status = 'superseded',
				attempts = attempts - 1,
				lease_token = NULL,
				lease_expires_at = NULL,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $3;
		`, iTopic.ExceptionQueueTable(exception.TopicId))
	} else {
		// updated CTE + INSERT keeps the mark and its delivery_log_<topic_id>
		// row atomic
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordSuperseded
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'superseded',
					attempts = attempts - 1,
					lease_token = NULL,
					lease_expires_at = NULL,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
					AND lease_token = $3
				RETURNING attempts
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT $1, $2, attempts + 1, 'superseded', $4
			FROM updated;
		`, iTopic.ExceptionQueueTable(exception.TopicId), iTopic.DeliveryLogTable(exception.TopicId))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken}
	if deliveryLogMode != topic.DeliveryLogModeOff {
		args = append(args, "a newer version of the same message key superseded this delivery")
	}
	return d.record(ctx, sql, args...)
}

// RecordDeferred returns the row to 'deferred': its key was busy when the
// claim reached the gate, so no run started. The claim's attempts increment
// is decremented back and the log row lands at that attempt; the next claim
// takes the row once the key frees.
func (d *ExceptionConsumerGroupDatastore) RecordDeferred(ctx context.Context, exception *ExceptionData, deliveryLogMode topic.DeliveryLogMode) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordDeferred(ctx, exception, deliveryLogMode)
	})
}

func (d *ExceptionConsumerGroupDatastore) recordDeferred(ctx context.Context, exception *ExceptionData, deliveryLogMode topic.DeliveryLogMode) error {
	var sql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordDeferred
			UPDATE %s
			SET
				status = 'deferred',
				attempts = attempts - 1,
				lease_token = NULL,
				lease_expires_at = NULL,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $3;
		`, iTopic.ExceptionQueueTable(exception.TopicId))
	} else {
		// updated CTE + INSERT keeps the mark and its delivery_log_<topic_id>
		// row atomic
		sql = fmt.Sprintf(`
			-- vulkan: exceptionconsumer.recordDeferred
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'deferred',
					attempts = attempts - 1,
					lease_token = NULL,
					lease_expires_at = NULL,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
					AND lease_token = $3
				RETURNING attempts
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT $1, $2, attempts + 1, 'deferred', $4
			FROM updated;
		`, iTopic.ExceptionQueueTable(exception.TopicId), iTopic.DeliveryLogTable(exception.TopicId))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken}
	if deliveryLogMode != topic.DeliveryLogModeOff {
		args = append(args, "another delivery held the message key when this claim reached its gate")
	}
	return d.record(ctx, sql, args...)
}

// recordAndReleaseKey records outcome and releases keyClaim
// in the same transaction.
func (d *ExceptionConsumerGroupDatastore) recordAndReleaseKey(ctx context.Context, keyClaim *KeyLeaseData, sql string, args ...any) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}

	releaseSql := fmt.Sprintf(`
		-- vulkan: exceptionconsumer.recordAndReleaseKey
		DELETE FROM %s
		WHERE consumer_group_id = $1
			AND message_key = $2
			AND lease_token = $3;
	`, iTopic.MessageKeyLeaseTable(keyClaim.TopicId))
	releaseTag, err := tx.Exec(ctx, releaseSql, keyClaim.ConsumerGroupId, keyClaim.MessageKey, keyClaim.Token)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if releaseTag.RowsAffected() == 0 {
		// the run outlived its key lease -- another delivery on the key may
		// have overlapped it
		d.Logger.WarnContext(ctx, "key lease expired mid-run and was taken over", "group_id", keyClaim.ConsumerGroupId, "message_key", keyClaim.MessageKey)
	}
	if tag.RowsAffected() == 0 {
		return common.ErrLeaseLost
	}
	return nil
}

func (d *ExceptionConsumerGroupDatastore) record(ctx context.Context, sql string, args ...any) error {
	tag, err := d.Datastore.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return common.ErrLeaseLost
	}
	return nil
}
