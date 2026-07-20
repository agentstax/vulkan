package consumer

// The LIFECYCLE path's datastore half, PARKED -- see consumer_lifecycle.go's
// header for why and what would revive it.

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// FanOut materializes one delivery row per message this group is bound to
// receive. Scans only above the group's mark (cursor.committed), so
// steady-state cost is O(new messages) per tick, not O(whole log).
func (d *ConsumerDatastore[Message]) FanOut(ctx context.Context, topicID int64, consumerGroup string, limit int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.fanOut(ctx, topicID, consumerGroup, limit)
	})
}

func (d *ConsumerDatastore[Message]) fanOut(ctx context.Context, topicID int64, consumerGroup string, limit int) error {
	// take the (head, xmax) pair the scan statement's gate below proves
	// against -- the same fence as FreshClaimMessagesWithCursor.
	snapshotSql := fmt.Sprintf(`
		SELECT
			(SELECT COALESCE(MAX(id), 0) FROM %s) AS head,
			pg_snapshot_xmax(pg_current_snapshot())::text AS xmax,
			c.committed,
			c.pending_head
		FROM cursor c
		WHERE c.consumer_group = $1 AND c.topic_id = $2;
	`, topic.MessageLogTable(topicID))

	var snapshotHead, committed, pendingHead int64
	var snapshotXmax string
	if err := d.Datastore.Pool.QueryRow(ctx, snapshotSql, consumerGroup, topicID).Scan(&snapshotHead, &snapshotXmax, &committed, &pendingHead); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no cursor for group %s on topic %d -- was Register called?", consumerGroup, topicID)
		}
		return err
	}

	// nothing visible above the mark and the stored pair already covers this
	// head -- the scan statement would find nothing and change nothing.
	if snapshotHead <= committed && snapshotHead <= pendingHead {
		return nil
	}

	scanSql := fmt.Sprintf(`
		WITH old_values AS (
			SELECT * FROM cursor
			WHERE consumer_group = $1 AND topic_id = $2
			-- FOR UPDATE so a racing same-group peer's committed advance is
			-- visible to our scan start (same race as the cursor claim path)
			FOR UPDATE
		),
		batch AS (
			-- the scan runs EAGERLY past the proven mark: a visible row whose
			-- neighbor below is still uncommitted materializes this tick anyway,
			-- because the delivery PK + ON CONFLICT DO NOTHING makes rescanning
			-- it next tick a no-op. only the mark advance below needs the proof.
			--
			-- every log row above the mark counts against the LIMIT, matched by
			-- the group's bindings or not -- so the mark still advances through
			-- rows this group skips.
			--
			-- the mark bound must stay a scalar subquery: it plans as an
			-- InitPlan feeding an index cond, O(batch). joining old_values in
			-- plans the same bound as a join FILTER over an in-id-order index
			-- walk from 0 -- O(whole log) per tick, measured 660x slower at 200k
			SELECT m.id, m.routing_key, m.compaction_key
			FROM %[2]s m                                           -- [2] = message_log table
			WHERE m.id > (SELECT committed FROM old_values)
			ORDER BY m.id
			LIMIT $3
		),
		materialized AS (
			INSERT INTO %[1]s (consumer_group, message_id, status) -- [1] = delivery table
			SELECT $1, b.id, 'ready'
			FROM batch b
			WHERE (
				-- no bindings for (consumer_group, topic_id) exists
				NOT EXISTS (
					SELECT 1 FROM binding bi
					WHERE bi.consumer_group = $1 AND bi.topic_id = $2
				)
				-- bindings for (consumer_group, topic_id) exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM binding bi
					WHERE bi.consumer_group = $1 AND bi.topic_id = $2
						AND b.routing_key ~ bi.pattern
				)
				-- if bindings exist but our routing_key does not match any of them
				-- no row is materialized for this message at all
			)
			AND (
				-- unkeyed rows are never compacted
				b.compaction_key IS NULL
				-- keyed rows materialize a delivery only if they're latest_key's
				-- current pointer for their key -- O(1) lookup, no per-row scan
				OR b.id = (
					SELECT latest_id FROM latest_key
					WHERE topic_id = $2
						AND compaction_key = b.compaction_key
				)
			)
			ON CONFLICT DO NOTHING
		),
		gate AS (
			-- how far the mark may advance: the best head proven by THIS
			-- statement's snapshot ($4/$5 is the fresh pair, pending_* the
			-- stored one -- see FreshClaimMessagesWithCursor for the proof).
			--
			-- unlike the claim gate there is NO settled_head term. the mark and
			-- the scan share one snapshot, and a proof is only usable here if
			-- everything under its head is VISIBLE to that snapshot -- true for
			-- both pair checks (they prove against this statement's own xmin),
			-- but not for a cached head a peer proved after our snapshot began:
			-- advancing to it would jump the mark past rows our scan never saw.
			-- when neither pair proves, o.committed keeps the mark in place and
			-- the next tick rescans -- held rows re-materialize as no-ops.
			SELECT GREATEST(
				o.committed,
				CASE WHEN pg_snapshot_xmin(pg_current_snapshot()) >= $5::xid8 -- $5 is snapshotXmax
					THEN $4 ELSE 0 END,                                         -- $4 is snapshotHead
				CASE WHEN o.pending_xmax IS NOT NULL
						AND pg_snapshot_xmin(pg_current_snapshot()) >= o.pending_xmax
					THEN o.pending_head ELSE 0 END
			) AS head
			FROM old_values o
		),
		mark AS (
			-- a full batch means the LIMIT cut the scan short -- cap the mark at
			-- the last id actually scanned so unscanned rows above it stay
			-- above the mark for the next tick.
			--
			-- EX: limit=50, committed=0, gate proves 200, 200 visible rows
			--   batch = ids 1-50, FULL -- rows 51-200 are visible but unscanned
			--   advancing to 200 would skip them forever -> cap at LEAST(200, 50)
			SELECT
				CASE WHEN (SELECT COUNT(*) FROM batch) = $3                   -- $3 is limit
				THEN LEAST(gate.head, (SELECT MAX(id) FROM batch))
				ELSE gate.head END
				AS head
			FROM gate
		)
		UPDATE cursor SET
			committed = mark.head,
			-- claimed rides along equal to committed: a fanout group hands out
			-- work per delivery row, never through a claimed/committed window.
			-- GREATEST for a group mistakenly claiming on BOTH paths -- its
			-- claim frontier must never regress to the mark (overlap = double
			-- delivery); its own committed staying monotonic is already given
			claimed = GREATEST(cursor.claimed, mark.head),
			-- store the fresh pair for the next tick: its txns will have
			-- finished by then, making it the next provable head.
			-- GREATEST so a racing peer's older pair can't overwrite a newer one
			pending_head = GREATEST(cursor.pending_head, $4),
			pending_xmax = GREATEST(cursor.pending_xmax, $5::xid8) -- also skips the initial NULL
		FROM mark
		WHERE cursor.consumer_group = $1 AND cursor.topic_id = $2;
	`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID))

	tag, err := d.Datastore.Pool.Exec(ctx, scanSql, consumerGroup, topicID, limit, snapshotHead, snapshotXmax)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// cursor row deleted between the two statements
		return fmt.Errorf("no cursor for group %s on topic %d -- was Register called?", consumerGroup, topicID)
	}

	return nil
}

func (d *ConsumerDatastore[Message]) ClaimMessagesWithLifecycle(ctx context.Context, topicID int64, consumerGroup string, limit int) ([]DeliveryRow, error) {
	var deliveries []DeliveryRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		deliveries, err = d.claimMessagesWithLifecycle(ctx, topicID, consumerGroup, limit)
		return err
	})
	return deliveries, err
}

func (d *ConsumerDatastore[Message]) claimMessagesWithLifecycle(ctx context.Context, topicID int64, consumerGroup string, limit int) ([]DeliveryRow, error) {
	// Claim this group's own delivery rows and move them 'ready' -> 'processing' in
	// one statement (the Phase 2 state machine, now per-(group, topic, message) instead
	// of per-message). SKIP LOCKED keeps competing workers from grabbing the same row.
	//
	// delivery only stores message_id, not the payload, so we join this topic's
	// message_log back in -- the log stays immutable, all mutation lives in delivery.
	//
	// Phase 6 deliberately has no lease: a 'processing' row that never gets resolved
	// (consumer crash) just sits there. Visibility-timeout reclaim is Phase 6.5.
	sql := fmt.Sprintf(`
		WITH claimed AS (
			UPDATE %[1]s
			SET
				status = 'processing',
				attempts = attempts + 1,
				updated_at = now()
			WHERE (consumer_group, message_id) IN (
				SELECT consumer_group, message_id FROM %[1]s
				WHERE consumer_group = $1
					AND status = 'ready'
				ORDER BY message_id
				LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			RETURNING consumer_group, message_id, status, attempts
		)
		SELECT
			c.consumer_group,
			$3::bigint AS topic_id,
			c.message_id,
			c.status,
			c.attempts,
			m.payload
		FROM claimed c
		JOIN %[2]s m ON m.id = c.message_id
		ORDER BY c.message_id;
	`, topic.DeliveryTable(topicID), topic.MessageLogTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, sql, consumerGroup, limit, topicID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, pgx.RowToStructByName[DeliveryRow])
}

// RecordSuccess marks a claimed delivery 'done'. Terminal success for this
// (group, message); the log row is untouched and other groups are unaffected.
func (d *ConsumerDatastore[Message]) RecordSuccess(ctx context.Context, delivery *DeliveryRow) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordSuccess(ctx, delivery)
	})
}

func (d *ConsumerDatastore[Message]) recordSuccess(ctx context.Context, delivery *DeliveryRow) error {
	sql := fmt.Sprintf(`
		UPDATE %s
		SET
			status = 'done',
			last_error = NULL,
			updated_at = now()
		WHERE consumer_group = $1
			AND message_id = $2;
	`, topic.DeliveryTable(delivery.TopicID))

	_, err := d.Datastore.Pool.Exec(ctx, sql, delivery.ConsumerGroup, delivery.MessageId)
	return err
}

// RecordFailure handles a processing error: retry until attempts are exhausted,
// then hand off to RecordTerminal (the per-group DLQ). attempts was already
// incremented at claim time, so >= maxAttempts means this was the last try.
// Phase 6 has no backoff (the delivery table carries no can_run_after) -- a
// 'ready' row is simply re-claimed on the next poll.
func (d *ConsumerDatastore[Message]) RecordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryRow, failureErr error, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordFailure(ctx, maxAttempts, delivery, failureErr, disableDeliveryLog)
	})
}

func (d *ConsumerDatastore[Message]) recordFailure(ctx context.Context, maxAttempts int, delivery *DeliveryRow, failureErr error, disableDeliveryLog bool) error {
	if delivery.Attempts >= maxAttempts {
		// private call, not the exported RecordTerminal -- this already runs
		// inside RecordFailure's own Retry.Wrap, calling the exported one
		// would nest a second retry loop around the same round-trip.
		return d.recordTerminal(ctx, delivery, failureErr, disableDeliveryLog)
	}

	var sql string
	args := []any{delivery.ConsumerGroup, delivery.MessageId, failureErr.Error()}
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'ready',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group = $1
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
				WHERE consumer_group = $1
					AND message_id = $2
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group, message_id, attempt, error)
			SELECT $1, $2, $4, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, topic.DeliveryTable(delivery.TopicID), topic.DeliveryLogTable(delivery.TopicID))
		args = append(args, delivery.Attempts)
	}

	_, err := d.Datastore.Pool.Exec(ctx, sql, args...)
	return err
}

// RecordTerminal dead-letters a delivery: no more retries. The DLQ for a group is
// just `WHERE consumer_group = $1 AND status = 'dead'`; one group can dead-letter a
// message while another processes the same offset fine.
func (d *ConsumerDatastore[Message]) RecordTerminal(ctx context.Context, delivery *DeliveryRow, terminalErr error, disableDeliveryLog bool) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordTerminal(ctx, delivery, terminalErr, disableDeliveryLog)
	})
}

func (d *ConsumerDatastore[Message]) recordTerminal(ctx context.Context, delivery *DeliveryRow, terminalErr error, disableDeliveryLog bool) error {
	var sql string
	args := []any{delivery.ConsumerGroup, delivery.MessageId, terminalErr.Error()}
	if disableDeliveryLog {
		sql = fmt.Sprintf(`
			UPDATE %s
			SET
				status = 'dead',
				last_error = $3,
				updated_at = now()
			WHERE consumer_group = $1
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
				WHERE consumer_group = $1
					AND message_id = $2
				RETURNING 1
			)
			INSERT INTO %[2]s (consumer_group, message_id, attempt, error)
			SELECT $1, $2, $4, $3
			WHERE EXISTS (SELECT 1 FROM updated);
		`, topic.DeliveryTable(delivery.TopicID), topic.DeliveryLogTable(delivery.TopicID))
		args = append(args, delivery.Attempts)
	}

	if _, err := d.Datastore.Pool.Exec(ctx, sql, args...); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, "message dead-lettered", "group", delivery.ConsumerGroup, "topic_id", delivery.TopicID, "message_id", delivery.MessageId, "error", terminalErr)
	return nil
}
