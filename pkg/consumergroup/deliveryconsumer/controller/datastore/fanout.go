package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

// FanOut materializes one delivery row per message this group is bound to
// receive. Scans only above the group's mark (cursor.committed), so
// steady-state cost is O(new messages) per tick, not O(whole log).
func (d *DeliveryConsumerGroupDatastore) FanOut(ctx context.Context, topicId int64, groupId int64, schemaVersion int64, limit int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.fanOut(ctx, topicId, groupId, schemaVersion, limit)
	})
}

func (d *DeliveryConsumerGroupDatastore) fanOut(ctx context.Context, topicId int64, groupId int64, schemaVersion int64, limit int) error {
	// take the (head, xmax) pair the scan statement's gate below proves
	// against.
	snapshotSql := fmt.Sprintf(`
		-- vulkan: deliveryconsumer.fanOut
		SELECT
			(SELECT COALESCE(MAX(id), 0) FROM %[1]s.%[2]s) AS head,
			pg_snapshot_xmax(pg_current_snapshot())::text AS xmax,
			c.committed,
			c.pending_head
		FROM %[1]s.%[3]s c
		WHERE c.consumer_group_id = $1;
	`, d.Datastore.Schema, topic.MessageLogTable(topicId), topic.ConsumerGroupCursorTable(topicId))

	var snapshotHead, committed, pendingHead int64
	var snapshotXmax string
	if err := d.Datastore.Pool.QueryRow(ctx, snapshotSql, groupId).Scan(&snapshotHead, &snapshotXmax, &committed, &pendingHead); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no cursor for group %d on topic %d -- was Register called?", groupId, topicId)
		}
		return err
	}

	// nothing visible above the mark and the stored pair already covers this
	// head -- the scan statement would find nothing and change nothing.
	if snapshotHead <= committed && snapshotHead <= pendingHead {
		return nil
	}

	scanSql := fmt.Sprintf(`
		-- vulkan: deliveryconsumer.fanOut
		WITH old_values AS (
			SELECT committed, pending_head, pending_xmax
			FROM %[1]s.%[4]s                                             -- [4] = consumer_group_cursor table
			WHERE consumer_group_id = $1
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
			SELECT m.id, m.schema_version, m.routing_key, m.message_key, m.compaction_rank, m.options
			FROM %[1]s.%[3]s m                                           -- [3] = message_log table
			WHERE m.id > (SELECT committed FROM old_values)
			ORDER BY m.id
			LIMIT $2
		),
		materialized AS (
			INSERT INTO %[1]s.%[2]s (consumer_group_id, message_id, status, message_key, concurrency) -- [2] = exception_queue table
			SELECT $1, b.id, 'ready', b.message_key, COALESCE(b.options->>'concurrency', 'parallel')
			FROM batch b
			-- rows at another payload version advance the mark without a delivery row
			WHERE b.schema_version = $5
			AND (
				-- no bindings for consumer_group exists
				NOT EXISTS (
					SELECT 1 FROM %[1]s.%[5]s bi                         -- [5] = binding_config table
					WHERE bi.consumer_group_id = $1
				)
				-- bindings for consumer_group exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM %[1]s.%[5]s bi
					WHERE bi.consumer_group_id = $1
						AND b.routing_key ~ bi.pattern_regex
				)
				-- if bindings exist but our routing_key does not match any of them
				-- no row is materialized for this message at all
			)
			AND (
				-- uncompacted rows (keyless or keyed) are never superseded
				b.compaction_rank IS NULL
				-- compacted rows materialize a delivery only if they're
				-- compaction_head's current pointer for their key -- O(1) lookup,
				-- no per-row scan
				OR b.id = (
					SELECT head_id FROM %[1]s.%[6]s                      -- [6] = compaction_head table
					WHERE compaction_key = b.message_key
				)
			)
			ON CONFLICT DO NOTHING
		),
		gate AS (
			-- how far the mark may advance: the best head proven by THIS
			-- statement's snapshot ($3/$4 is the fresh pair, pending_* the
			-- stored one).
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
				CASE WHEN pg_snapshot_xmin(pg_current_snapshot()) >= $4::xid8 -- $4 is snapshotXmax
					THEN $3 ELSE 0 END,                                         -- $3 is snapshotHead
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
				CASE WHEN (SELECT COUNT(*) FROM batch) = $2                   -- $2 is limit
				THEN LEAST(gate.head, (SELECT MAX(id) FROM batch))
				ELSE gate.head END
				AS head
			FROM gate
		)
		UPDATE %[1]s.%[4]s c SET
			committed = mark.head,
			-- claimed rides along equal to committed: a fanout group hands out
			-- work per delivery row, never through a claimed/committed window.
			-- GREATEST for a group mistakenly claiming on BOTH paths -- its
			-- claim frontier must never regress to the mark (overlap = double
			-- delivery); its own committed staying monotonic is already given
			claimed = GREATEST(c.claimed, mark.head),
			-- store the fresh pair for the next tick: its txns will have
			-- finished by then, making it the next provable head.
			-- GREATEST so a racing peer's older pair can't overwrite a newer one
			pending_head = GREATEST(c.pending_head, $3),
			pending_xmax = GREATEST(c.pending_xmax, $4::xid8) -- also skips the initial NULL
		FROM mark
		WHERE c.consumer_group_id = $1;
	`, d.Datastore.Schema, topic.ExceptionQueueTable(topicId), topic.MessageLogTable(topicId), topic.ConsumerGroupCursorTable(topicId), topic.BindingConfigTable(topicId), topic.CompactionHeadTable(topicId))

	tag, err := d.Datastore.Pool.Exec(ctx, scanSql, groupId, limit, snapshotHead, snapshotXmax, schemaVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// cursor row deleted between the two statements
		return fmt.Errorf("no cursor for group %d on topic %d -- was Register called?", groupId, topicId)
	}

	return nil
}
