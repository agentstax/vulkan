package waterline

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// EnsureGroup creates the single-lane cursor (lane 0, block_hi NULL = cap at the
// live head) for a group if it does not exist. Use InitLanes (sharding.go) for
// the multi-lane escape hatch.
func (l *PgLog) EnsureGroup(ctx context.Context, group string) error {
	_, err := l.Pool.Exec(ctx,
		`INSERT INTO cursors(consumer_group, lane) VALUES ($1, 0) ON CONFLICT DO NOTHING`, group)
	return err
}

// readRange reads the events of (lo, hi] that THIS group is bound to receive. A
// group with no binding receives all events (routing is opt-in, Phase 7). The
// routing predicate is pushed into SQL so non-matching payloads are never even
// transferred — but the cursor still advances over the whole contiguous block
// (so committed stays a dense frontier; non-matching offsets are "resolved" with
// no work and no exception row).
func (l *PgLog) readRange(ctx context.Context, q pgx.Tx, group string, lo, hi int64) ([]Event, error) {
	const read = `
		SELECT e."offset", e.topic, e.routing_key, e.partition_key, e.payload
		FROM events e
		WHERE e."offset" > $1 AND e."offset" <= $2
		  AND ( NOT EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group = $3)
		     OR EXISTS (SELECT 1 FROM bindings b
		                 WHERE b.consumer_group = $3
		                   AND ( (b.kind = 'topic'  AND e.routing_key ~ b.pattern)
		                      OR (b.kind = 'header' AND e.headers @> b.header_match) )) )
		ORDER BY e."offset";`
	rows, err := q.Query(ctx, read, lo, hi, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEvents(rows)
}

func collectEvents(rows pgx.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Offset, &e.Topic, &e.RoutingKey, &e.PartitionKey, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Claim reserves the next batch on a lane (advances `claimed`, writes a lease)
// and returns the routed events to process. It caps at the lane's FROZEN block
// (block_hi) when sharded, else the live head (R1/R4: a sharded claim never
// reads past its block, so K lanes never overlap or leave a seam). An empty
// range means the lane is caught up.
func (l *PgLog) Claim(ctx context.Context, group string, lane, batch int, lease time.Duration) (Range, []Event, error) {
	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return Range{}, nil, err
	}
	defer tx.Rollback(ctx)

	// Advance the read frontier, capped at COALESCE(block_hi, head). old/new gives (lo, hi].
	const reserve = `
		WITH h AS (SELECT COALESCE(max("offset"), 0) AS head FROM events)
		UPDATE cursors c
		SET claimed = LEAST(c.claimed + $3, COALESCE(c.block_hi, h.head))
		FROM h
		WHERE c.consumer_group = $1 AND c.lane = $2
		  AND c.claimed < COALESCE(c.block_hi, h.head)
		RETURNING old.claimed AS lo, new.claimed AS hi;` // PG18 old/new RETURNING
	r := Range{Group: group, Lane: lane, Lo: -1}
	if err := tx.QueryRow(ctx, reserve, group, lane, batch).Scan(&r.Lo, &r.Hi); err != nil {
		if errors.Is(err, pgx.ErrNoRows) { // caught up
			return Range{Lo: -1}, nil, tx.Commit(ctx)
		}
		return Range{}, nil, err
	}

	// Record the lease so a crash before Commit is reclaimable (d5).
	const leaseQ = `
		INSERT INTO leases(consumer_group, lane, lo, hi, lease_until, lease_token)
		VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5), gen_random_uuid())
		RETURNING lease_token;`
	if err := tx.QueryRow(ctx, leaseQ, group, lane, r.Lo, r.Hi, lease.Seconds()).Scan(&r.Token); err != nil {
		return Range{}, nil, err
	}

	evs, err := l.readRange(ctx, tx, group, r.Lo, r.Hi)
	if err != nil {
		return Range{}, nil, err
	}
	return r, evs, tx.Commit(ctx)
}

// Reclaim grabs ONE expired lease and ROTATES its token in a single atomic
// statement (R5). FOR UPDATE SKIP LOCKED stops two reclaimers double-grabbing
// it; the rotated token makes the original slow worker's stale Commit a no-op
// (R3). Workers should try Reclaim before Claim so crashed ranges drain first.
//
// Poison-batch cap (R5 [5]): a batch whose processing CRASHES the worker (not
// merely fails) leaves the lease and is reclaimed again and again, pinning the
// waterline forever. After maxReclaims reclaims, Reclaim QUARANTINES the range —
// it parks the routed offsets into the exception window (per-message isolation,
// where the crash-loop backstop dead-letters the actual poison) and frees the
// lease — then returns "nothing reclaimed". maxReclaims<=0 disables the cap.
func (l *PgLog) Reclaim(ctx context.Context, group string, lease time.Duration, maxReclaims int) (Range, []Event, bool, error) {
	const q = `
		UPDATE leases SET lease_until = now() + make_interval(secs => $2),
		                  lease_token = gen_random_uuid(), reclaims = reclaims + 1
		WHERE (consumer_group, lane, lo) IN (
			SELECT consumer_group, lane, lo FROM leases
			WHERE consumer_group = $1 AND lease_until < now()
			ORDER BY lo FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING lane, lo, hi, lease_token, reclaims;`
	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return Range{}, nil, false, err
	}
	defer tx.Rollback(ctx)

	r := Range{Group: group, Lo: -1}
	var reclaims int
	if err := tx.QueryRow(ctx, q, group, lease.Seconds()).Scan(&r.Lane, &r.Lo, &r.Hi, &r.Token, &reclaims); err != nil {
		if errors.Is(err, pgx.ErrNoRows) { // nothing to reclaim
			return Range{Lo: -1}, nil, false, tx.Commit(ctx)
		}
		return Range{}, nil, false, err
	}
	if maxReclaims > 0 && reclaims > maxReclaims {
		const quarantine = `
			INSERT INTO deliveries(consumer_group, lane, "offset", partition_key, state, attempts, last_error)
			SELECT $1, $2, e."offset", e.partition_key, 'ready', 0, 'reclaim cap exceeded (poison batch quarantined)'
			FROM events e
			WHERE e."offset" > $3 AND e."offset" <= $4
			  AND ( NOT EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group=$1)
			     OR EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group=$1
			                AND ( (b.kind='topic'  AND e.routing_key ~ b.pattern)
			                   OR (b.kind='header' AND e.headers @> b.header_match) )) )
			ON CONFLICT (consumer_group, "offset") DO NOTHING;`
		if _, err := tx.Exec(ctx, quarantine, group, r.Lane, r.Lo, r.Hi); err != nil {
			return Range{}, nil, false, err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM leases WHERE consumer_group=$1 AND lane=$2 AND lo=$3`, group, r.Lane, r.Lo); err != nil {
			return Range{}, nil, false, err
		}
		return Range{Lo: -1}, nil, false, tx.Commit(ctx)
	}
	evs, err := l.readRange(ctx, tx, group, r.Lo, r.Hi)
	if err != nil {
		return Range{}, nil, false, err
	}
	return r, evs, true, tx.Commit(ctx)
}

// Commit closes a reserved range in ONE transaction. R3 FIX: free the lease
// FIRST (token-guarded); abort with ErrLeaseLost if we no longer own it, parking
// NOTHING (a stale/slow worker must not inject phantom exception rows). Only if
// we still owned it do we park the failures. R6 FIX: the park uses ON CONFLICT
// DO UPDATE so a re-park advances attempts toward dead instead of being silently
// dropped, and never clobbers a leased or already-dead row.
func (l *PgLog) Commit(ctx context.Context, r Range, exc []Exception) error {
	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const free = `DELETE FROM leases WHERE consumer_group=$1 AND lane=$2 AND lo=$3 AND lease_token=$4;`
	tag, err := tx.Exec(ctx, free, r.Group, r.Lane, r.Lo, r.Token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost // reclaimer owns this range now — do nothing
	}

	if len(exc) > 0 {
		offs := make([]int64, len(exc))
		errs := make([]string, len(exc))
		for i, e := range exc {
			offs[i], errs[i] = e.Offset, e.Err
		}
		// attempts starts at 0 (consistent with Materialize): `attempts` counts
		// EXCEPTION-DRAIN attempts, incremented at ClaimExceptions time. The
		// happy-path miss is recorded in last_error, not as an attempt (R6 [7]).
		const park = `
			INSERT INTO deliveries(consumer_group, lane, "offset", partition_key, state, attempts, available_at, last_error)
			SELECT $1, $2, t.o, e.partition_key, 'ready', 0, now() + interval '5 seconds', t.e
			FROM unnest($3::bigint[], $4::text[]) AS t(o, e)
			LEFT JOIN events e ON e."offset" = t.o
			ON CONFLICT (consumer_group, "offset") DO UPDATE
			   SET last_error = EXCLUDED.last_error,
			       available_at = GREATEST(deliveries.available_at, now())
			 WHERE deliveries.state <> 'dead' AND deliveries.lease_token IS NULL;`
		if _, err := tx.Exec(ctx, park, r.Group, r.Lane, offs, errs); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// reapExpiredSQL dead-letters expired-inflight rows whose attempts already hit
// maxAttempts. This is the crash-loop backstop: a message that kills the worker
// PROCESS never reaches Nack, so the dead transition must be made WITHOUT user
// code — here, at reclaim time. Without it, a process-crashing poison message
// would be reclaimed forever and wedge the waterline (R6 liveness).
const reapExpiredSQL = `
	UPDATE deliveries SET state='dead', lease_token=NULL, lease_until=NULL,
	       last_error = COALESCE(last_error,'') || ' [reaped: crash-loop hit max attempts]'
	 WHERE consumer_group=$1 AND state='inflight' AND lease_until < now() AND attempts >= $2;`

// ClaimExceptions claims up to n exceptions (-> inflight, SKIP LOCKED), returning
// the rows AND the events to reprocess (evs[i] is aligned to ds[i]). It first
// reaps crash-looped rows to dead, then RECLAIMS still-eligible expired-inflight
// rows (a crashed exception worker) folded into the claim, exactly like the
// Phase 2 lease reclamation — so no crash, transient, or poison message can leave
// an 'inflight' row that wedges the waterline. attempts++ on every (re)claim.
// This is the unordered drain; ClaimPartitioned is the FIFO-per-key variant.
func (l *PgLog) ClaimExceptions(ctx context.Context, group string, n, maxAttempts int, lease time.Duration) ([]Delivery, []Event, error) {
	if _, err := l.Pool.Exec(ctx, reapExpiredSQL, group, maxAttempts); err != nil {
		return nil, nil, err
	}
	const q = `
		UPDATE deliveries d
		   SET state='inflight', lease_token=gen_random_uuid(),
		       lease_until=now()+make_interval(secs=>$3), attempts=attempts+1
		 WHERE (d.consumer_group, d."offset") IN (
		     SELECT dd.consumer_group, dd."offset" FROM deliveries dd
		      WHERE dd.consumer_group=$1 AND dd.available_at<=now()
		        AND ( dd.state='ready'
		           OR (dd.state='inflight' AND dd.lease_until < now()) )  -- reclaim crashed worker
		      ORDER BY dd."offset" FOR UPDATE SKIP LOCKED LIMIT $2)
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

// Ack (exception success) — pop-delete: the row vanishes, so Advance has nothing
// to prune. Token-guarded: a stale Ack after reclaim is a no-op (ErrLeaseLost).
func (l *PgLog) Ack(ctx context.Context, d *Delivery) error {
	const q = `DELETE FROM deliveries WHERE consumer_group=$1 AND "offset"=$2 AND lease_token=$3;`
	tag, err := l.Pool.Exec(ctx, q, d.Group, d.Offset, d.LeaseToken)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// AckBatch pop-deletes a whole batch of just-claimed exceptions in ONE statement
// = ONE commit/fsync. This is the throughput path that matches the SQL
// "pop-delete b1000" benchmark: a per-message Ack pays one fsync per message (the
// Phase 3.5 commit wall), while batching the successes collapses B fsyncs into
// one (Kafka offset commits / SQS DeleteMessageBatch do the same). It is
// TOKEN-GUARDED per row (offset+token), exactly like the single Ack: a delivery
// that was reclaimed by another worker (rotated token) is left for its new owner,
// never deleted out from under it. Returns the number actually deleted.
func (l *PgLog) AckBatch(ctx context.Context, ds []Delivery) (int64, error) {
	if len(ds) == 0 {
		return 0, nil
	}
	offs := make([]int64, len(ds))
	toks := make([]pgtype.UUID, len(ds))
	group := ds[0].Group
	for i := range ds {
		offs[i], toks[i] = ds[i].Offset, ds[i].LeaseToken
	}
	const q = `
		DELETE FROM deliveries d
		USING unnest($2::bigint[], $3::uuid[]) AS t(off, tok)
		WHERE d.consumer_group=$1 AND d."offset"=t.off AND d.lease_token=t.tok;`
	tag, err := l.Pool.Exec(ctx, q, group, offs, toks)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DrainPopDelete is the raw fused pop-delete primitive the SQL benchmark
// measured (~258k @ b1000): it DELETEs up to n ready exceptions and RETURNs
// their events in ONE statement (one commit), with no inflight lease. It trades
// the crash-safe lease for that single commit, so it is correct only when
// processing is idempotent/trivial — a crash after the delete but before
// processing loses the message. The leased ClaimExceptions + AckBatch path is
// the crash-safe default; this is the throughput ceiling for comparison.
func (l *PgLog) DrainPopDelete(ctx context.Context, group string, n int) ([]Event, error) {
	const q = `
		WITH popped AS (
			DELETE FROM deliveries
			 WHERE (consumer_group, "offset") IN (
				SELECT consumer_group, "offset" FROM deliveries
				 WHERE consumer_group=$1 AND state='ready' AND available_at<=now()
				 ORDER BY "offset" FOR UPDATE SKIP LOCKED LIMIT $2)
			RETURNING "offset")
		SELECT e."offset", e.topic, e.routing_key, e.partition_key, e.payload
		FROM popped p JOIN events e ON e."offset" = p."offset"
		ORDER BY e."offset";`
	rows, err := l.Pool.Query(ctx, q, group, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectEvents(rows)
}

// Nack — R6: token-guarded in-place UPDATE, NEVER an INSERT (a stale Nack cannot
// resurrect an Acked/deleted offset). At maxAttempts the row goes 'dead' (which
// unblocks committed); otherwise back to 'ready' with backoff.
func (l *PgLog) Nack(ctx context.Context, maxAttempts int, d *Delivery, cause error) error {
	const q = `
		UPDATE deliveries
		   SET state = CASE WHEN attempts >= $4 THEN 'dead'::delivery_state ELSE 'ready'::delivery_state END,
		       lease_token = NULL, lease_until = NULL, last_error = $5,
		       available_at = now() + make_interval(secs => $6)
		 WHERE consumer_group=$1 AND "offset"=$2 AND lease_token=$3;`
	tag, err := l.Pool.Exec(ctx, q, d.Group, d.Offset, d.LeaseToken, maxAttempts, cause.Error(), backoff(d.Attempts))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// DeadLetter — terminal, retained below the line as the per-group DLQ.
func (l *PgLog) DeadLetter(ctx context.Context, d *Delivery, cause error) error {
	const q = `UPDATE deliveries SET state='dead', lease_token=NULL, lease_until=NULL, last_error=$4
	            WHERE consumer_group=$1 AND "offset"=$2 AND lease_token=$3;`
	tag, err := l.Pool.Exec(ctx, q, d.Group, d.Offset, d.LeaseToken, cause.Error())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Advance slides the waterline to the lowest blocker on this lane: the lowest
// OPEN LEASE (an in-flight range) or the lowest UNRESOLVED exception
// (ready|inflight) ON THIS LANE (R2 lane-scoped). dead rows do NOT block (they
// are resolved); successful offsets left no row. If neither blocks, committed =
// claimed. GREATEST keeps committed monotonic under concurrent rollers.
func (l *PgLog) Advance(ctx context.Context, group string, lane int) (int64, error) {
	const q = `
		UPDATE cursors c
		SET committed = GREATEST(c.committed,
			LEAST(
				(SELECT min(lo)         FROM leases     WHERE consumer_group=$1 AND lane=$2),
				(SELECT min("offset")-1 FROM deliveries WHERE consumer_group=$1 AND lane=$2
				                         AND state IN ('ready','inflight')),
				c.claimed))               -- LEAST ignores NULLs; claimed clamps committed to the read frontier
		WHERE c.consumer_group=$1 AND c.lane=$2
		RETURNING committed;`
	var committed int64
	err := l.Pool.QueryRow(ctx, q, group, lane).Scan(&committed)
	return committed, err
}

// Watermark = the group's cumulative guarantee: the largest W such that EVERY
// offset <= W is resolved by the group. Because lanes own CONTIGUOUS blocks in
// lane (= offset) order, this is the committed of the FIRST lane that has not yet
// reached its block_hi (everything below it is dense), or the highest block cap
// if every lane is complete. (min(committed) would be wrong here: once lane 0
// finishes, min sticks at head/k while the true waterline keeps rising — so
// CaughtUp could never fire for a sharded group.) For the single-lane case
// (block_hi NULL) this reduces to that lane's committed.
func (l *PgLog) Watermark(ctx context.Context, group string) (int64, error) {
	const q = `
		SELECT COALESCE(
			(SELECT c.committed FROM cursors c
			  WHERE c.consumer_group=$1
			    AND c.committed < COALESCE(c.block_hi, (SELECT COALESCE(max("offset"),0) FROM events))
			  ORDER BY c.lane LIMIT 1),
			(SELECT max(COALESCE(c.block_hi, c.committed)) FROM cursors c WHERE c.consumer_group=$1),
			0);`
	var wm int64
	err := l.Pool.QueryRow(ctx, q, group).Scan(&wm)
	return wm, err
}

// eventsFor loads the log events for a set of deliveries and returns them
// ALIGNED to ds: result[i] corresponds to ds[i] (same offset). The DB's
// UPDATE ... RETURNING order is NOT the offset order, so we must pair by offset,
// not by row position — otherwise a worker would process one offset's payload
// while holding another offset's lease (mis-ack/nack). A delivery whose event was
// compacted away yields an Event with that Offset and a nil Payload, so the
// caller can detect it (and dead-letter it) rather than silently misalign.
func (l *PgLog) eventsFor(ctx context.Context, ds []Delivery) ([]Event, error) {
	if len(ds) == 0 {
		return nil, nil
	}
	offs := make([]int64, len(ds))
	for i := range ds {
		offs[i] = ds[i].Offset
	}
	rows, err := l.Pool.Query(ctx,
		`SELECT "offset", topic, routing_key, partition_key, payload FROM events
		  WHERE "offset" = ANY($1)`, offs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found, err := collectEvents(rows)
	if err != nil {
		return nil, err
	}
	byOff := make(map[int64]Event, len(found))
	for _, e := range found {
		byOff[e.Offset] = e
	}
	aligned := make([]Event, len(ds))
	for i := range ds {
		if e, ok := byOff[ds[i].Offset]; ok {
			aligned[i] = e
		} else {
			aligned[i] = Event{Offset: ds[i].Offset} // event gone (e.g. compacted); Payload nil
		}
	}
	return aligned, nil
}

func collectDeliveries(rows pgx.Rows) ([]Delivery, error) {
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var tok pgtype.UUID
		if err := rows.Scan(&d.Group, &d.Lane, &d.Offset, &d.PartitionKey, &d.Attempts, &tok); err != nil {
			return nil, err
		}
		d.State = Inflight
		d.LeaseToken = tok
		out = append(out, d)
	}
	return out, rows.Err()
}

// backoff returns the retry delay in seconds for the given attempt count:
// exponential (base 2s) capped at 5 minutes.
func backoff(attempts int) float64 {
	const base, cap = 2.0, 300.0
	d := base * math.Pow(2, float64(attempts))
	if d > cap {
		return cap
	}
	return d
}
