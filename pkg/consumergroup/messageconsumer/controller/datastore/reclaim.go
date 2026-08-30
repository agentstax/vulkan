package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// grab ONE expired lease and re-read its exact range so a worker that crashed
// mid-range doesn't strand those offsets. past maxRangeReclaims the range is
// POISON -- quarantine it into the sparse exception window instead of handing it
// out again, so one bad message can't crash-loop the whole range forever.
func (d *MessageConsumerGroupDatastore) reclaimWithCursor(ctx context.Context, topicId int64, groupId int64, schemaVersion int64, maxRangeReclaims int, leaseDuration time.Duration, deliveryLogMode topic.DeliveryLogMode) (*ClaimedRangeData, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// a single in-place UPDATE, not delete+insert -- reclaims accumulates on the
	// SAME row instead of resetting to 0 every time. token still rotates, so a
	// dead worker's stale commit still no-ops the same as before.
	reclaimSql := fmt.Sprintf(`
		-- vulkan: messageconsumer.reclaimWithCursor
		UPDATE %s
		SET
			reclaims = reclaims + 1,
			expires_at = now() + make_interval(secs => $2),
			token = gen_random_uuid()
		WHERE (token, consumer_group_id) IN (
			SELECT token, consumer_group_id FROM %s
			WHERE consumer_group_id = $1
				AND expires_at < now()
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING
			token,
			consumer_group_id,
			low,
			high,
			expires_at,
			reclaims;
	`, iTopic.ClaimLeaseTable(topicId), iTopic.ClaimLeaseTable(topicId))
	leaseRows, err := tx.Query(ctx, reclaimSql, groupId, leaseDuration.Seconds())
	if err != nil {
		return nil, err
	}

	lease, err := pgx.CollectOneRow(leaseRows, pgx.RowToStructByName[LeaseData])
	if err != nil {
		// no reclaimable leases were found -> follow normal claim from message_log
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	d.Logger.WarnContext(ctx, consumergroup.EventLeaseReclaimed.Message, "code", consumergroup.EventLeaseReclaimed.Code, "group_id", groupId, "topic_id", topicId, "low", lease.Low, "high", lease.High, "reclaims", lease.Reclaims)

	if lease.Reclaims >= maxRangeReclaims {
		if err := d.quarantine(ctx, tx, topicId, groupId, lease, deliveryLogMode); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		// the lease is freed and every row written as a 'ready' exception
		return &ClaimedRangeData{Lease: lease, Quarantined: true}, nil
	}

	messages, err := d.readMessages(ctx, tx, topicId, groupId, schemaVersion, lease.Low, lease.High)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &ClaimedRangeData{Lease: lease, Messages: messages}, nil
}

// quarantine gives up on retrying a poisoned range as one unit: every message in
// it is written as an independent 'ready' exception (attempts starts fresh at 0 -- a
// separate retry budget from the range's now-exhausted reclaim count), each
// logging its own delivery_log_<topic_id> row same as any other exception write, and the
// lease frees for good. From here each message lives or dies on its own via the
// exact same exception-window machinery as an ordinary consumed-message failure --
// AdvanceCommitted's exception-blocker term pins committed on whichever
// resolves last, so one bad message no longer holds up its siblings forever.
func (d *MessageConsumerGroupDatastore) quarantine(ctx context.Context, tx pgx.Tx, topicId int64, groupId int64, lease LeaseData, deliveryLogMode topic.DeliveryLogMode) error {
	d.Logger.WarnContext(ctx, consumergroup.EventRangeQuarantined.Message, "code", consumergroup.EventRangeQuarantined.Code, "group_id", groupId, "topic_id", topicId, "low", lease.Low, "high", lease.High, "reclaims", lease.Reclaims)

	var deliverySql string
	if deliveryLogMode == topic.DeliveryLogModeOff {
		deliverySql = fmt.Sprintf(`
			-- vulkan: messageconsumer.quarantine
			INSERT INTO %s (
				consumer_group_id,
				message_id,
				status,
				message_key,
				concurrency,
				attempts,
				last_error
			)
			SELECT
				$1,
				id,
				'ready',
				message_key,
				COALESCE(options->>'concurrency', 'parallel'),
				0,
				'quarantined: range reclaimed too many times'
			FROM %s
			WHERE id > $2
				AND id <= $3;
		`, iTopic.ExceptionQueueTable(topicId), iTopic.MessageLogTable(topicId))
	} else {
		// inserted CTE + INSERT keeps the range-wide write and its delivery_log_<topic_id>
		// rows atomic -- one log row per message written, same first-recorded-attempt
		// convention (attempt=0) as commit's own log statement.
		deliverySql = fmt.Sprintf(`
			-- vulkan: messageconsumer.quarantine
			WITH inserted AS (
				INSERT INTO %[1]s (
					consumer_group_id,
					message_id,
					status,
					message_key,
					concurrency,
					attempts,
					last_error
				)
				SELECT
					$1,
					id,
					'ready',
					message_key,
					COALESCE(options->>'concurrency', 'parallel'),
					0,
					'quarantined: range reclaimed too many times'
				FROM %[2]s
				WHERE id > $2
					AND id <= $3
				RETURNING message_id, last_error
			)
			INSERT INTO %[3]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, message_id, 0, last_error FROM inserted;
		`, iTopic.ExceptionQueueTable(topicId), iTopic.MessageLogTable(topicId), iTopic.DeliveryLogTable(topicId))
	}
	if _, err := tx.Exec(ctx, deliverySql, groupId, lease.Low, lease.High); err != nil {
		return err
	}

	freeSql := fmt.Sprintf(`
		-- vulkan: messageconsumer.quarantine
		DELETE FROM %s
		WHERE consumer_group_id = $1
			AND token = $2;
	`, iTopic.ClaimLeaseTable(topicId))
	_, err := tx.Exec(ctx, freeSql, groupId, lease.Token)
	return err
}

// ForceReclaimRange surrenders a range nobody ever started -- unlike
// PartialCommit this expires the WHOLE lease immediately so the next
// reclaim can pick it straight back up.
func (d *MessageConsumerGroupDatastore) ForceReclaimRange(ctx context.Context, topicId int64, groupId int64, token pgtype.UUID) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.forceReclaimRange(ctx, topicId, groupId, token)
	})
}

func (d *MessageConsumerGroupDatastore) forceReclaimRange(ctx context.Context, topicId int64, groupId int64, token pgtype.UUID) error {
	// reclaims goes negative on purpose: the next reclaimWithCursor's
	// unconditional +1 nets it back to 0 -- this must not count as a real reclaim.
	sql := fmt.Sprintf(`
		-- vulkan: messageconsumer.forceReclaimRange
		UPDATE %s
		SET
			expires_at = now(),
			reclaims = GREATEST(reclaims - 1, -1), -- should never go under -1
			token = gen_random_uuid()              -- rotate token so any retry matches 0 rows instead of double decrementing
		WHERE consumer_group_id = $1
			AND token = $2;
	`, iTopic.ClaimLeaseTable(topicId))
	tag, err := d.Datastore.Pool.Exec(ctx, sql, groupId, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return common.ErrLeaseLost
	}
	return nil
}
