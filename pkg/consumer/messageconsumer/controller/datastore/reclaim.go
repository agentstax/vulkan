package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	consumerbase "github.com/agentstax/vulkan/pkg/consumer/base"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// grab ONE expired lease and re-read its exact range so a worker that crashed
// mid-range doesn't strand those offsets. past maxRangeReclaims the range is
// POISON -- quarantine it into the sparse exception window instead of handing it
// out again, so one bad message can't crash-loop the whole range forever.
func (d *MessageConsumerDatastore) reclaimWithCursor(ctx context.Context, topicID int64, groupID int64, maxRangeReclaims int, leaseDuration time.Duration, disableDeliveryLog bool) (*ClaimedRangeData, error) {
	tx, err := d.Datastore.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// a single in-place UPDATE, not delete+insert -- reclaims accumulates on the
	// SAME row instead of resetting to 0 every time. token still rotates, so a
	// dead worker's stale commit still no-ops the same as before.
	reclaimSql := `
		UPDATE lease
		SET
			reclaims = reclaims + 1,
			until = now() + make_interval(secs => $2),
			token = gen_random_uuid()
		WHERE (token, consumer_group_id) IN (
			SELECT token, consumer_group_id FROM lease
			WHERE consumer_group_id = $1
				AND until < now()
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *;
	`

	leaseRows, err := tx.Query(ctx, reclaimSql, groupID, leaseDuration.Seconds())
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

	d.Logger.InfoContext(ctx, "lease reclaimed from expired worker", "group_id", groupID, "topic_id", topicID, "low", lease.Low, "high", lease.High, "reclaims", lease.Reclaims)

	if lease.Reclaims >= maxRangeReclaims {
		if err := d.quarantine(ctx, tx, topicID, groupID, lease, disableDeliveryLog); err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}

	messages, err := d.readMessages(ctx, tx, topicID, groupID, lease.Low, lease.High)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &ClaimedRangeData{Lease: lease, Messages: messages}, nil
}

// quarantine gives up on retrying a poisoned range as one unit: every message in
// it parks as an independent 'ready' exception (attempts starts fresh at 0 -- a
// separate retry budget from the range's now-exhausted reclaim count), each
// logging its own delivery_log_<topic_id> row same as any other park, and the
// lease frees for good. From here each message lives or dies on its own via the
// exact same exception-window machinery as an ordinary consumed-message failure --
// AdvanceWaterline's exception-blocker term pins committed on whichever
// resolves last, so one bad message no longer holds up its siblings forever.
func (d *MessageConsumerDatastore) quarantine(ctx context.Context, tx pgx.Tx, topicID int64, groupID int64, lease LeaseData, disableDeliveryLog bool) error {
	d.Logger.WarnContext(ctx, "range quarantined after max reclaims, messages parked as exceptions", "group_id", groupID, "topic_id", topicID, "low", lease.Low, "high", lease.High, "reclaims", lease.Reclaims)

	var parkSql string
	if disableDeliveryLog {
		parkSql = fmt.Sprintf(`
			INSERT INTO %s (consumer_group_id, message_id, status, attempts, last_error)
			SELECT $1, id, 'ready', 0, 'quarantined: range reclaimed too many times'
			FROM %s
			WHERE id > $2
				AND id <= $3;
		`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID))
	} else {
		// parked CTE + INSERT keeps the range-wide park and its delivery_log_<topic_id>
		// rows atomic -- one log row per message parked, same first-recorded-attempt
		// convention (attempt=0) as commit's own log statement.
		parkSql = fmt.Sprintf(`
			WITH parked AS (
				INSERT INTO %[1]s (consumer_group_id, message_id, status, attempts, last_error)
				SELECT $1, id, 'ready', 0, 'quarantined: range reclaimed too many times'
				FROM %[2]s
				WHERE id > $2
					AND id <= $3
				RETURNING message_id, last_error
			)
			INSERT INTO %[3]s (consumer_group_id, message_id, attempt, error)
			SELECT $1, message_id, 0, last_error FROM parked;
		`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID), topic.DeliveryLogTable(topicID))
	}
	if _, err := tx.Exec(ctx, parkSql, groupID, lease.Low, lease.High); err != nil {
		return err
	}

	freeSql := `
		DELETE FROM lease
		WHERE consumer_group_id = $1
			AND token = $2;
	`
	_, err := tx.Exec(ctx, freeSql, groupID, lease.Token)
	return err
}

// ForceReclaimRange surrenders a range nobody ever started -- unlike
// PartialCommit this expires the WHOLE lease immediately so the next
// reclaim can pick it straight back up.
func (d *MessageConsumerDatastore) ForceReclaimRange(ctx context.Context, groupID int64, token pgtype.UUID) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.forceReclaimRange(ctx, groupID, token)
	})
}

func (d *MessageConsumerDatastore) forceReclaimRange(ctx context.Context, groupID int64, token pgtype.UUID) error {
	// reclaims goes negative on purpose: the next reclaimWithCursor's
	// unconditional +1 nets it back to 0 -- this must not count as a real reclaim.
	sql := `
		UPDATE lease
		SET
			until = now(),
			reclaims = GREATEST(reclaims - 1, -1), -- should never go under -1
			token = gen_random_uuid()              -- rotate token so any retry matches 0 rows instead of double decrementing
		WHERE consumer_group_id = $1
			AND token = $2;
	`

	tag, err := d.Datastore.Pool.Exec(ctx, sql, groupID, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return consumerbase.ErrLeaseLost
	}
	return nil
}
