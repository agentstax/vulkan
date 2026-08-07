package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/internal/topic"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/agentstax/vulkan/pkg/retry"
)

// RecordExceptionSuccess deletes the row.
// A non-nil keyClaim frees the key in the same transaction.
func (d *ExceptionConsumerDatastore) RecordExceptionSuccess(ctx context.Context, exception *ExceptionData, keyClaim *KeyLeaseData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordExceptionSuccess(ctx, exception, keyClaim)
	})
}

func (d *ExceptionConsumerDatastore) recordExceptionSuccess(ctx context.Context, exception *ExceptionData, keyClaim *KeyLeaseData) error {
	sql := fmt.Sprintf(`
		DELETE FROM %s
		WHERE consumer_group_id = $1
			AND message_id = $2
			AND lease_token = $3;
	`, topic.DeliveryTable(exception.TopicID))

	if keyClaim == nil {
		return d.record(ctx, sql, exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken)
	}
	return d.recordAndReleaseKey(ctx, keyClaim, sql, exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken)
}

// RecordExceptionFailure resets delivery so it can be retried or marked 'dead'.
// A non-nil keyClaim frees the key in the same transaction.
func (d *ExceptionConsumerDatastore) RecordExceptionFailure(ctx context.Context, retryPolicy *retry.Policy, exception *ExceptionData, failureErr error, disableDeliveryLog bool, keyClaim *KeyLeaseData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordExceptionFailure(ctx, retryPolicy, exception, failureErr, disableDeliveryLog, keyClaim)
	})
}

func (d *ExceptionConsumerDatastore) recordExceptionFailure(ctx context.Context, retryPolicy *retry.Policy, exception *ExceptionData, failureErr error, disableDeliveryLog bool, keyClaim *KeyLeaseData) error {
	if exception.Attempts >= retryPolicy.MaxRetries {
		return d.recordExceptionTerminal(ctx, exception, failureErr, disableDeliveryLog, keyClaim)
	}

	// clears the lease so it's claimable as a fresh 'ready' retry once can_run_after passes.
	var sql string
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'ready',
				lease_token = NULL,
				lease_until = NULL,
				last_error = $3,
				can_run_after = now() + make_interval(secs => $4),
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $5;
		`, topic.DeliveryTable(exception.TopicID))
	} else {
		sql = fmt.Sprintf(`
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'ready',
					lease_token = NULL,
					lease_until = NULL,
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
		`, topic.DeliveryTable(exception.TopicID), topic.DeliveryLogTable(exception.TopicID))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, failureErr.Error(), retryPolicy.CalculateDelay(exception.Attempts - 1).Seconds(), exception.LeaseToken}
	if !disableDeliveryLog {
		args = append(args, exception.Attempts)
	}

	if keyClaim == nil {
		return d.record(ctx, sql, args...)
	}
	return d.recordAndReleaseKey(ctx, keyClaim, sql, args...)
}

// RecordExceptionTerminal marks the row 'dead' -- no retry could succeed.
// A non-nil keyClaim frees the key in the same transaction.
func (d *ExceptionConsumerDatastore) RecordExceptionTerminal(ctx context.Context, exception *ExceptionData, failureErr error, disableDeliveryLog bool, keyClaim *KeyLeaseData) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordExceptionTerminal(ctx, exception, failureErr, disableDeliveryLog, keyClaim)
	})
}

func (d *ExceptionConsumerDatastore) recordExceptionTerminal(ctx context.Context, exception *ExceptionData, failureErr error, disableDeliveryLog bool, keyClaim *KeyLeaseData) error {
	var sql string
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'dead',
				lease_token = NULL,
				lease_until = NULL,
				last_error = $3,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $4;
		`, topic.DeliveryTable(exception.TopicID))
	} else {
		// updated CTE + INSERT ... WHERE EXISTS keeps the UPDATE and its
		// delivery_log_<topic_id> row atomic
		sql = fmt.Sprintf(`
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'dead',
					lease_token = NULL,
					lease_until = NULL,
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
		`, topic.DeliveryTable(exception.TopicID), topic.DeliveryLogTable(exception.TopicID))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, failureErr.Error(), exception.LeaseToken}
	if !disableDeliveryLog {
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

	d.Logger.WarnContext(ctx, "exception dead-lettered (unrecoverable, will not be retried)", "group_id", exception.ConsumerGroupId, "topic_id", exception.TopicID, "message_id", exception.MessageId, "attempts", exception.Attempts)
	return nil
}

// RecordExceptionSuperseded never runs the row again: the claim's attempts
// increment is decremented back and the log row lands at that attempt.
func (d *ExceptionConsumerDatastore) RecordExceptionSuperseded(ctx context.Context, exception *ExceptionData, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordExceptionSuperseded(ctx, exception, disableDeliveryLog)
	})
}

func (d *ExceptionConsumerDatastore) recordExceptionSuperseded(ctx context.Context, exception *ExceptionData, disableDeliveryLog bool) error {
	var sql string
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'superseded',
				attempts = attempts - 1,
				lease_token = NULL,
				lease_until = NULL,
				updated_at = now()
			WHERE consumer_group_id = $1
				AND message_id = $2
				AND lease_token = $3;
		`, topic.DeliveryTable(exception.TopicID))
	} else {
		// updated CTE + INSERT keeps the mark and its delivery_log_<topic_id>
		// row atomic
		sql = fmt.Sprintf(`
			WITH updated AS (
				UPDATE %[1]s
				SET
					status = 'superseded',
					attempts = attempts - 1,
					lease_token = NULL,
					lease_until = NULL,
					updated_at = now()
				WHERE consumer_group_id = $1
					AND message_id = $2
					AND lease_token = $3
				RETURNING attempts
			)
			INSERT INTO %[2]s (consumer_group_id, message_id, attempt, status, error)
			SELECT $1, $2, attempts + 1, 'superseded', $4
			FROM updated;
		`, topic.DeliveryTable(exception.TopicID), topic.DeliveryLogTable(exception.TopicID))
	}

	args := []any{exception.ConsumerGroupId, exception.MessageId, exception.LeaseToken}
	if !disableDeliveryLog {
		args = append(args, "a newer message on the same compaction key superseded this delivery")
	}
	return d.record(ctx, sql, args...)
}

// recordAndReleaseKey records outcome and releases keyClaim
// in the same transaction.
func (d *ExceptionConsumerDatastore) recordAndReleaseKey(ctx context.Context, keyClaim *KeyLeaseData, sql string, args ...any) error {
	if keyClaim == nil {
		return errors.New("recordAndReleaseKey requires a key claim -- use record for keyless outcomes")
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}

	releaseSql := `
		DELETE FROM key_lease
		WHERE consumer_group_id = $1
			AND compaction_key = $2
			AND lease_token = $3;
	`
	releaseTag, err := tx.Exec(ctx, releaseSql, keyClaim.ConsumerGroupId, keyClaim.CompactionKey, keyClaim.Token)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if releaseTag.RowsAffected() == 0 {
		// the run outlived its key lease -- another delivery on the key may
		// have overlapped it
		d.Logger.WarnContext(ctx, "key lease expired mid-run and was taken over", "group_id", keyClaim.ConsumerGroupId, "compaction_key", keyClaim.CompactionKey)
	}
	if tag.RowsAffected() == 0 {
		return consumerbase.ErrLeaseLost
	}
	return nil
}

func (d *ExceptionConsumerDatastore) record(ctx context.Context, sql string, args ...any) error {
	tag, err := d.Datastore.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return consumerbase.ErrLeaseLost
	}
	return nil
}
