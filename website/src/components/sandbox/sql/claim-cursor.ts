// verbatim from pkg/consumergroup/messageconsumer/controller/datastore/freshclaim.go
// freshClaimMessagesWithCursor -- the template is drift-checked byte-exact; the
// function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { consumerGroupCursorTable } from './table-names';

export const claimCursorSqlTemplate = `
		-- vulkan: messageconsumer.freshClaimMessagesWithCursor
		WITH old_values AS ( -- PG18+ has old / new syntax in returning but we want older version compatibility so use CTE
			SELECT
				claimed,
				settled_head,
				pending_head,
				pending_xmax
			FROM %s
			WHERE consumer_group_id = $1
			-- must FOR UPDATE, get race if using a basic snapshot read
			-- two same-group workers racing on one cursor row (claimed=0, head=200, limit=100):
			--
			--   worker A: claims (0, 100], txn still open
			--   worker B: takes its snapshot (claimed=0), blocks on A's row lock
			--   worker A: commits
			--   worker B: unblocks; its UPDATE re-checks the row's LATEST version
			--             (claimed=100), so high is correct: 100+100 = 200
			--
			-- but B's low comes from THIS select, and forks on its read mode:
			--
			--   snapshot read:  low = 0   (stale)  -> B returns (0, 200]   -> overlaps A
			--   FOR UPDATE:     low = 100 (latest) -> B returns (100, 200] -> disjoint
			FOR UPDATE
		),
		gate AS (
			-- gate = how far claimed may advance this poll.
			--
			-- the raw MAX(id) is unsafe: BIGSERIAL issues ids at INSERT time,
			-- txns commit in any order, and nothing re-reads below claimed:
			--
			-- EX:
			--
			--   producer A: INSERT id=8, txn stays open
			--   producer B: INSERT id=9, commits
			--   claim to MAX(id)=9 -> reads (0,9], 8 is invisible, skipped
			--   producer A commits -> 8 < claimed forever -> LOST
			--
			-- the fix: claimed only advances to a head PROVEN to have nothing
			-- invisible at or below it. the proof works on a (head, xmax)
			-- pair -- MAX(id) (head) and the next-unissued txid (max), read together
			-- in one EARLIER snapshot (snapshotSql above, or a prior poll that
			-- stored its pair in pending_head/pending_xmax).
			--
			-- EX: proving the pair (head=9, xmax=103) from snapshotSql:
			--
			--   1. the pair says:  every txn that can own an id <= 9 has txid < 103
			--                      (all ids <= 9 were INSERTed before txid 103 was issued)
			--   2. since then:     txns have kept finishing, so xmin -- the oldest
			--                      txid still running -- rises toward 103 as they do
			--   3. this query:     if it sees xmin >= 103, every txid < 103 is finished
			--   4. therefore:      every txn that can own an id <= 9 is finished --
			--                      anything committing at or below 9 already has, so
			--                      claiming through 9 skips nothing
			--
			-- gate takes the best proven head available:
			--   settled_head     -- wins when neither pair proves (a txn seen by
			--                       both snapshots is still open, e.g. a long
			--                       ProduceInTx) -- claims hold at the last proven
			--                       head until it closes
			--   the fresh pair   -- $3/$4, wins when everything running at
			--                       snapshotSql finished before this query ran --
			--                       the quiet path, claims land in the same poll
			--                       as the produce
			--   the stored pair  -- wins under nonstop traffic: the fresh pair is
			--                       only microseconds old, too young for its fenced
			--                       txns to have finished, but the stored pair has
			--                       had a full poll interval for that -- xmin has
			--                       passed its xmax. claiming through it claims up
			--                       to where the log stood a poll ago, so fresh
			--                       messages wait one more poll if this is used
			SELECT GREATEST(
				o.settled_head,
				CASE WHEN pg_snapshot_xmin(pg_current_snapshot()) >= $4::xid8 -- $4 is snapshotXmax
					THEN $3 ELSE 0 END,                                         -- $3 is snapshotHead
				CASE WHEN o.pending_xmax IS NOT NULL
						AND pg_snapshot_xmin(pg_current_snapshot()) >= o.pending_xmax
					THEN o.pending_head ELSE 0 END
			) AS head
			FROM old_values o
		),
		updated AS (
			UPDATE %s c
			SET
				-- advance by up to batchLimit, capped at the proven head.
				claimed = LEAST(c.claimed + $2, gate.head),
				-- cache this poll's proof: a later poll where neither pair
				-- proves claims up to this instead.
				settled_head = gate.head,
				-- store the fresh pair for the next poll: ideally its txns will
				-- have finished by then, making it the next provable head.
				-- GREATEST so a racing peer's older pair can't overwrite a newer one
				pending_head = GREATEST(c.pending_head, $3),
				pending_xmax = GREATEST(c.pending_xmax, $4::xid8) -- also skips the initial NULL
			FROM old_values, gate
			WHERE c.consumer_group_id = $1
			RETURNING
				old_values.claimed AS low,
				c.claimed AS high
		)
		-- updated always fires when the cursor row exists (the pending columns
		-- store unconditionally), so:
		--
		--   state                        | rows   low    high   meaning
		--   claimed=100, proven=200      | 1      100    200    claim (100, 200]
		--   claimed=200, proven=200      | 1      200    200    caught up (low = high)
		--   no cursor row                | 0      -      -      row deleted since the
		--                                                       snapshot read it -> error
		--
		SELECT u.low, u.high FROM updated u;
	`;

export function claimCursorSql(topicId: number): string {
	return interpolate(
		claimCursorSqlTemplate,
		consumerGroupCursorTable(topicId),
		consumerGroupCursorTable(topicId),
	);
}
