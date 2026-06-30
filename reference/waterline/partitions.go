package waterline

import (
	"context"
	"time"
)

// FIFO partitions (Phase 8). Ordering is opt-in and paid for only where used.
//
// Design choice (hybrid-consistent): the cheap claim-from-log happy path
// (Claim/Commit) is the UNORDERED, max-throughput fan-out — it reserves
// contiguous ranges and processes them concurrently, so it gives no per-key
// ordering guarantee under multiple workers. A stream that needs ordering opts
// into the LIFECYCLE path instead: Materialize a delivery row per event (this is
// literally the Phase 6 fan-out projector), then drain with ClaimPartitioned,
// which enforces "at most one in-flight per key" AND FIFO-through-retry. Keyed
// events serialize; NULL-key events parallelize fully — both on the same stream.
//
// Why a separate path: a dense, contiguous cursor cannot selectively defer one
// key's offset while advancing past its neighbours, so per-key ordering lives in
// the deliveries layer (where each offset has its own row and state), not in the
// frontier-lane layer (which shards contiguous blocks for read throughput, a
// different axis). This matches the design doc's note that striping a growing
// log needs "a per-stripe dense sequence ... a different structure".

// Materialize creates a 'ready' delivery row for every event above the group's
// materialized frontier (cursors.claimed, lane 0) that the group is bound to
// receive, then advances that frontier to the frozen head. Idempotent: already
// materialized-and-acked offsets sit below the frontier and are never recreated.
// Returns the number of new rows. This is the Phase 6 per-(group,event) fan-out;
// ClaimPartitioned (below) adds the Phase 8 ordering gate on top of it.
//
// A group is EITHER a happy-path (Claim/Commit) consumer OR a FIFO/Materialize
// consumer — not both. Both drive cursors.claimed on lane 0, so mixing them on
// one group corrupts the frontier. Pick one mode per group; the waterline
// (committed) is still advanced lazily by Advance in both modes, so Watermark,
// CaughtUp, and CompactSafe stay correct as long as a roller runs Advance.
func (l *PgLog) Materialize(ctx context.Context, group string) (int64, error) {
	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var head int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max("offset"),0) FROM events`).Scan(&head); err != nil {
		return 0, err
	}
	const ins = `
		INSERT INTO deliveries(consumer_group, "offset", partition_key, state)
		SELECT $1, e."offset", e.partition_key, 'ready'
		FROM events e
		WHERE e."offset" > (SELECT claimed FROM cursors WHERE consumer_group=$1 AND lane=0)
		  AND e."offset" <= $2
		  AND ( NOT EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group=$1)
		     OR EXISTS (SELECT 1 FROM bindings b
		                 WHERE b.consumer_group=$1
		                   AND ( (b.kind='topic'  AND e.routing_key ~ b.pattern)
		                      OR (b.kind='header' AND e.headers @> b.header_match) )) )
		ON CONFLICT (consumer_group, "offset") DO NOTHING;`
	tag, err := tx.Exec(ctx, ins, group, head)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE cursors SET claimed=$2 WHERE consumer_group=$1 AND lane=0 AND claimed<$2`,
		group, head); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

// ClaimPartitioned claims up to n ready deliveries with FIFO-per-key semantics:
//   - at most one in-flight delivery per (non-null) key per group, and
//   - only the LOWEST unresolved offset of a key is eligible, so a key whose
//     head is failed-and-backing-off blocks its later offsets (FIFO survives a
//     retry). A NULL key carries no constraint -> full concurrency.
//
// A key whose head dead-letters stops blocking (dead leaves the ready|inflight
// set), so the next offset for that key becomes eligible — the DLQ does not wedge
// the partition forever. Like ClaimExceptions it reaps crash-looped rows to dead
// and RECLAIMS expired-inflight rows (a crashed FIFO worker); without that a
// crash would leave a live 'inflight' head and freeze its whole partition (the
// at-most-one-in-flight gate would never re-open). A LIVE in-flight (lease not
// expired) still blocks the key; only an EXPIRED one is reclaimable.
func (l *PgLog) ClaimPartitioned(ctx context.Context, group string, n, maxAttempts int, lease time.Duration) ([]Delivery, []Event, error) {
	if _, err := l.Pool.Exec(ctx, reapExpiredSQL, group, maxAttempts); err != nil {
		return nil, nil, err
	}
	const q = `
		UPDATE deliveries d
		   SET state='inflight', lease_token=gen_random_uuid(),
		       lease_until=now()+make_interval(secs=>$3), attempts=attempts+1
		 WHERE (d.consumer_group, d."offset") IN (
		     SELECT dd.consumer_group, dd."offset"
		     FROM deliveries dd
		     WHERE dd.consumer_group=$1 AND dd.available_at<=now()
		       AND ( dd.state='ready'
		          OR (dd.state='inflight' AND dd.lease_until < now()) )      -- reclaim crashed worker
		       AND ( dd.partition_key IS NULL
		          OR NOT EXISTS (SELECT 1 FROM deliveries x
		                          WHERE x.consumer_group=$1 AND x.partition_key=dd.partition_key
		                            AND x.state='inflight' AND x.lease_until >= now()) )  -- only LIVE in-flight blocks
		       AND ( dd.partition_key IS NULL
		          OR dd."offset" = (SELECT min(y."offset") FROM deliveries y
		                             WHERE y.consumer_group=$1 AND y.partition_key=dd.partition_key
		                               AND y.state IN ('ready','inflight')) )
		     ORDER BY dd."offset"
		     FOR UPDATE SKIP LOCKED
		     LIMIT $2)
		RETURNING d.consumer_group, d.lane, d."offset", d.partition_key, d.attempts, d.lease_token;`
	rows, err := l.Pool.Query(ctx, q, group, n, lease.Seconds())
	if err != nil {
		return nil, nil, err
	}
	ds, err := collectDeliveries(rows)
	if err != nil {
		return nil, nil, err
	}
	evs, err := l.eventsFor(ctx, ds)
	return ds, evs, err
}
