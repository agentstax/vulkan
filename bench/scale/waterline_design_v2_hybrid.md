# Waterline v2 — the hybrid managed cursor (benchmark-corrected)

Evolves the canonical-naming design (`cursors`/`deliveries`/`Advance`) with what the at-scale
benchmark + second audit proved. Docs keep the waterline metaphor; code uses the CS lineage.

## What changed from v1 (and why)

| v1 (original) | v2 (this doc) | benchmark reason |
|---|---|---|
| `Fanout` materializes a `deliveries` row for **every** offset | happy path **claims a range straight from the log**, writes **no** per-event row | per-event row write is the consume-side wall (~124k, degrades with table size); claim-from-log ran 460–770k |
| `deliveries` holds every (group,offset) | `deliveries` is a **sparse exception window** — a row exists only for retry/dead/out-of-order | pay the row cost only for the small fraction that needs lifecycle |
| ack success = `UPDATE state='acked'` | success = **`DELETE`** (pop-delete) | pop-delete doubled per-row drain (110k→258k); also removes the acked-row prune step |
| no crash-safe reclaim of in-flight work | **`leases`** row per claimed range (d5) | d4 silently skips a crashed range; d5 makes it reclaimable for ~3% |
| single frontier per group | frontier **shardable** into lanes (escape hatch) | sharding lifted a single hot group's wall 136k→521k (+280%) |
| `Advance` folds a contiguous run of acked rows | `Advance` floors `committed` at the lowest **open lease** or **unresolved exception** | the waterline is now bounded by in-flight ranges + exceptions, not materialized acks |

States collapse from `ready·inflight·acked·dead` to **`ready·inflight·dead`** — there is no `acked`,
because a successful exception is *deleted*, not marked. `dead` still persists below the line as the DLQ.

## Correctness status — adversarially reviewed & empirically tested (2026-06-21)

A 6-lens adversarial review found, and a Postgres harness (`waterline_v2.sql`, driver
`drivers/audit_waterline_v2.sh`, **10/10 assertions pass**) then confirmed, six real bugs in the
*first draft* of this doc. **All are fixed below.** Keep this list — they are the load-bearing
invariants:

- **R1 (gap/dup, sharding):** the draft `Claim` was lane-blind (capped at the *global* head, no lane
  window) — all K lanes claimed the same `(0,batch]` (measured: 4 lanes all returned `0–200`). Fixed:
  `Claim` clamps to a **frozen per-lane block** (`cursors.block_hi`); the unstable `offset%K` streaming
  mode is removed (a dense single-integer cursor can't represent a stripe). See §Sharding.
- **R2 (liveness):** `Advance`'s exception blocker was group-wide, so one lane's exception froze every
  lane (measured: lane-0 pinned to 300 by a lane-1 exception). Fixed: `deliveries` gains a `lane` column
  and the blocker is `AND lane=$2` (lane-fix reached the true frontier, 400).
- **R3 (double-process):** the draft `Commit` parked exceptions *before* the token-guarded lease free and
  ignored `RowsAffected`, so a slow/reclaimed worker injected phantom rows (measured: doc variant injects
  a `ready` row; safe variant returns `false`, no row). Fixed: free-the-lease-**first**, abort if not ours.
- **R4 (gap):** block boundaries were computed from a *live, growing* `max(offset)`. Fixed: freeze head
  per run into `cursors.block_hi`.
- **R5 (dup):** `Reclaim` was unspecified; "refresh" without token rotation let a stale worker free a live
  lease. Fixed: `Reclaim` is one atomic `FOR UPDATE SKIP LOCKED` + **token rotation** (measured: a locked
  lease is skipped by a second reclaimer).
- **R6 (liveness):** `Nack`/`DeadLetter` were unspecified and the park used `ON CONFLICT DO NOTHING`,
  freezing `attempts` and pinning the waterline. Fixed: bodies specified; park uses `DO UPDATE` that
  advances `attempts` toward `dead` and never clobbers a leased/dead row (measured: 50 always-failing
  exceptions all reach `dead` and `committed` then advances to head).

## Schema

```sql
-- Immutable log (unchanged).
CREATE TABLE events (
    "offset"   BIGSERIAL PRIMARY KEY,
    payload    JSONB        NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- One cursor per (group, lane). lane shards the claim frontier; lane=0 is the single-frontier case.
-- committed = waterline (every offset <= committed in this lane is resolved). claimed = read frontier.
CREATE TABLE cursors (
    consumer_group TEXT   NOT NULL,
    lane           INT    NOT NULL DEFAULT 0,
    committed      BIGINT NOT NULL DEFAULT 0,
    claimed        BIGINT NOT NULL DEFAULT 0,
    block_hi       BIGINT,                       -- R1/R4: frozen upper bound of this lane's block
    PRIMARY KEY (consumer_group, lane)           --        (NULL for the single-lane case = head)
);

-- In-flight reserved ranges (d5 crash-safe reclaim). A row exists ONLY while a batch is
-- claimed-but-not-committed; (lo, hi] is the range one worker holds.
CREATE TABLE leases (
    consumer_group TEXT        NOT NULL,
    lane           INT         NOT NULL,
    lo             BIGINT      NOT NULL,
    hi             BIGINT      NOT NULL,
    lease_until    TIMESTAMPTZ NOT NULL,
    lease_token    UUID        NOT NULL,
    PRIMARY KEY (consumer_group, lane, lo)
);

-- SPARSE exception window: a row exists ONLY for an offset that fell off the happy path.
-- Success on the happy path leaves NO row at all. 'dead' rows persist below the line = the DLQ.
CREATE TYPE delivery_state AS ENUM ('ready', 'inflight', 'dead');  -- note: no 'acked' (success = DELETE)

CREATE TABLE deliveries (
    consumer_group TEXT           NOT NULL,
    lane           INT            NOT NULL DEFAULT 0,   -- R2: which lane owns this exception
    "offset"       BIGINT         NOT NULL,
    state          delivery_state NOT NULL DEFAULT 'ready',
    attempts       INT            NOT NULL DEFAULT 0,
    available_at   TIMESTAMPTZ    NOT NULL DEFAULT now(),  -- retry/backoff gate
    lease_until    TIMESTAMPTZ,
    lease_token    UUID,
    last_error     TEXT,
    PRIMARY KEY (consumer_group, "offset")
);
-- claim index for the exception drain (pop-delete path): ready rows in offset order.
CREATE INDEX deliveries_ready ON deliveries (consumer_group, "offset")
    WHERE state = 'ready';
```

## Structs

```go
type DeliveryState string

const (
	Ready    DeliveryState = "ready"    // an exception awaiting (re)processing
	Inflight DeliveryState = "inflight" // an exception leased to a worker
	Dead     DeliveryState = "dead"     // dead-lettered; retained below the line as the DLQ
	// (no Acked: a successfully (re)processed exception is DELETEd, not marked)
)

// Event is one log record the happy path reads directly.
type Event struct {
	Offset  int64
	Payload []byte
}

// Range is a reserved [lo+1, hi] slice of the log held under a lease.
type Range struct {
	Group string
	Lane  int
	Lo    int64       // exclusive low (old frontier)
	Hi    int64       // inclusive high (new frontier)
	Token pgtype.UUID // lease token; guards commit against a stolen/expired lease
}

// Delivery is one (group, offset) row in the SPARSE exception window. Offsets resolved on
// the happy path have NO row; offsets at/below committed are represented by the cursor alone.
type Delivery struct {
	Group      string
	Offset     int64
	State      DeliveryState
	Attempts   int
	LeaseUntil *time.Time
	LeaseToken pgtype.UUID
	LastError  *string
}

// Cursor is one lane's progress: committed waterline + claimed read-frontier.
type Cursor struct {
	Group     string
	Lane      int
	Committed int64
	Claimed   int64
}
```

## Operations

```go
type Log[Msg any] interface {
	// ---- happy path: claim a RANGE from the log, no per-event rows ----

	// Claim reserves the next batch of offsets on a lane (advances `claimed`, writes a lease)
	// and returns the events to process. Empty range => nothing to do right now.
	Claim(ctx context.Context, group string, lane, batch int, lease time.Duration) (Range, []Event, error)

	// Reclaim returns an EXPIRED lease's range to be reprocessed (at-least-once after a crash),
	// refreshing its lease. Workers should try Reclaim before Claim.
	Reclaim(ctx context.Context, group string, lease time.Duration) (Range, []Event, bool, error)

	// Commit closes a reserved range in ONE transaction: insert exception rows for the offsets
	// that did NOT succeed, then delete the lease. (Waterline moves later, in Advance.)
	Commit(ctx context.Context, r Range, exceptions []Exception) error

	// ---- exception window: per-message lifecycle, drained pop-delete ----

	ClaimExceptions(ctx context.Context, group string, n int, lease time.Duration) ([]Delivery, []Event, error)
	Ack(ctx context.Context, d *Delivery) error                                  // success => DELETE (pop-delete)
	Nack(ctx context.Context, maxAttempts int, d *Delivery, cause error) error   // retry w/ backoff, or -> dead
	DeadLetter(ctx context.Context, d *Delivery, cause error) error              // terminal, retained as DLQ

	// ---- waterline roll-up (lazy, off the hot path) ----

	// Advance floors `committed` at the lowest open lease / lowest unresolved exception on the lane.
	// Idempotent; staleness only delays GC, never correctness. Returns the new committed.
	Advance(ctx context.Context, group string, lane int) (int64, error)

	// Watermark = the group's cumulative guarantee = min(committed) over all lanes.
	Watermark(ctx context.Context, group string) (int64, error)
}

// Exception is an offset that failed first-pass processing on the happy path.
type Exception struct {
	Offset int64
	Err    string
}
```

Representative implementations:

```go
// Claim — reserve the next range on a lane and read it straight from the log. No deliveries rows.
func (l *PgLog[Msg]) Claim(ctx context.Context, group string, lane, batch int, lease time.Duration) (Range, []Event, error) {
	tx, err := l.Pool.Begin(ctx)
	if err != nil { return Range{}, nil, err }
	defer tx.Rollback(ctx)

	// advance the read frontier, capped at the log head; old/new gives the (lo, hi] range.
	const reserve = `
		WITH h AS (SELECT COALESCE(max("offset"),0) AS head FROM events)
		UPDATE cursors c
		SET claimed = LEAST(c.claimed + $3, h.head)
		FROM h
		WHERE c.consumer_group=$1 AND c.lane=$2 AND c.claimed < h.head
		RETURNING old.claimed AS lo, new.claimed AS hi;`           -- PG18 old/new RETURNING
	var r Range
	r.Group, r.Lane = group, lane
	if err := tx.QueryRow(ctx, reserve, group, lane, batch).Scan(&r.Lo, &r.Hi); err != nil {
		if errors.Is(err, pgx.ErrNoRows) { tx.Commit(ctx); return Range{Lo: -1}, nil, nil } // caught up
		return Range{}, nil, err
	}
	// record the lease so a crash before Commit is reclaimable (d5).
	const lease_q = `
		INSERT INTO leases(consumer_group, lane, lo, hi, lease_until, lease_token)
		VALUES ($1,$2,$3,$4, now()+make_interval(secs=>$5), gen_random_uuid())
		RETURNING lease_token;`
	if err := tx.QueryRow(ctx, lease_q, group, lane, r.Lo, r.Hi, lease.Seconds()).Scan(&r.Token); err != nil {
		return Range{}, nil, err
	}
	// read the reserved events straight from the log (the real work to process).
	const read = `SELECT "offset", payload FROM events WHERE "offset" > $1 AND "offset" <= $2 ORDER BY "offset";`
	rows, err := tx.Query(ctx, read, r.Lo, r.Hi)
	if err != nil { return Range{}, nil, err }
	evs, err := pgx.CollectRows(rows, pgx.RowToStructByName[Event])
	if err != nil { return Range{}, nil, err }
	return r, evs, tx.Commit(ctx)
}

// Commit — R3 FIX: free the lease FIRST (token-guarded); only if WE still owned it do we park the
// failures. If the lease was reclaimed/expired (token mismatch) this is a no-op => ErrLeaseLost, and
// we park NOTHING (a stale/slow worker must not inject phantom exception rows). Verified by test T1.
func (l *PgLog[Msg]) Commit(ctx context.Context, r Range, exc []Exception) error {
	tx, err := l.Pool.Begin(ctx)
	if err != nil { return err }
	defer tx.Rollback(ctx)

	// 1) release the lease, guarded by our token. RowsAffected==0 => we lost it; abort the whole commit.
	const free = `DELETE FROM leases WHERE consumer_group=$1 AND lane=$2 AND lo=$3 AND lease_token=$4;`
	tag, err := tx.Exec(ctx, free, r.Group, r.Lane, r.Lo, r.Token)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return ErrLeaseLost }   // reclaimer owns this range now — do nothing

	// 2) we still owned it: park the failures. R6 FIX: DO UPDATE (not DO NOTHING) so a re-park advances
	// attempts toward dead instead of being silently dropped; never clobber a leased or already-dead row.
	if len(exc) > 0 {
		offs := make([]int64, len(exc)); errs := make([]string, len(exc))
		for i, e := range exc { offs[i], errs[i] = e.Offset, e.Err }
		const park = `
			INSERT INTO deliveries(consumer_group,lane,"offset",state,attempts,available_at,last_error)
			SELECT $1, $2, o, 'ready', 1, now() + interval '5 seconds', e
			FROM unnest($3::bigint[], $4::text[]) AS t(o, e)
			ON CONFLICT (consumer_group,"offset") DO UPDATE
			   SET attempts = deliveries.attempts + 1, last_error = EXCLUDED.last_error,
			       available_at = GREATEST(deliveries.available_at, now())
			 WHERE deliveries.state <> 'dead' AND deliveries.lease_token IS NULL;`
		if _, err := tx.Exec(ctx, park, r.Group, r.Lane, offs, errs); err != nil { return err }
	}
	return tx.Commit(ctx)
}

// Ack (exception success) — pop-delete: the row vanishes, so Advance has nothing to prune.
func (l *PgLog[Msg]) Ack(ctx context.Context, d *Delivery) error {
	const q = `DELETE FROM deliveries WHERE consumer_group=$1 AND "offset"=$2 AND lease_token=$3;`
	tag, err := l.Pool.Exec(ctx, q, d.Group, d.Offset, d.LeaseToken)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return ErrLeaseLost }
	return nil
}

// Reclaim — R5 FIX: grab ONE expired lease and ROTATE its token in a single atomic statement.
// FOR UPDATE SKIP LOCKED stops two reclaimers double-grabbing it (test T6); the rotated token makes the
// original slow worker's stale Commit a no-op (test T1). "refresh" ALWAYS means rotate-and-extend.
func (l *PgLog[Msg]) Reclaim(ctx context.Context, group string, lease time.Duration) (Range, []Event, bool, error) {
	const q = `
		UPDATE leases SET lease_until = now()+make_interval(secs=>$2), lease_token = gen_random_uuid()
		WHERE (consumer_group, lane, lo) IN (
			SELECT consumer_group, lane, lo FROM leases
			WHERE consumer_group=$1 AND lease_until < now()
			ORDER BY lo FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING lane, lo, hi, lease_token;`
	var r Range; r.Group = group
	if err := l.Pool.QueryRow(ctx, q, group, lease.Seconds()).Scan(&r.Lane, &r.Lo, &r.Hi, &r.Token); err != nil {
		if errors.Is(err, pgx.ErrNoRows) { return Range{Lo: -1}, nil, false, nil } // nothing to reclaim
		return Range{}, nil, false, err
	}
	const read = `SELECT "offset", payload FROM events WHERE "offset" > $1 AND "offset" <= $2 ORDER BY "offset";`
	rows, err := l.Pool.Query(ctx, read, r.Lo, r.Hi)
	if err != nil { return Range{}, nil, false, err }
	evs, err := pgx.CollectRows(rows, pgx.RowToStructByName[Event])
	return r, evs, err == nil, err
}

// Nack / DeadLetter — R6 FIX: token-guarded in-place UPDATE, NEVER an INSERT (so a stale Nack can't
// resurrect an Acked/deleted offset). At maxAttempts the row goes 'dead' (which unblocks committed).
func (l *PgLog[Msg]) Nack(ctx context.Context, maxAttempts int, d *Delivery, cause error) error {
	const q = `
		UPDATE deliveries
		   SET state = CASE WHEN attempts >= $4 THEN 'dead'::delivery_state ELSE 'ready'::delivery_state END,
		       lease_token = NULL, lease_until = NULL, last_error = $5,
		       available_at = now() + make_interval(secs => $6)
		 WHERE consumer_group=$1 AND "offset"=$2 AND lease_token=$3;`
	tag, err := l.Pool.Exec(ctx, q, d.Group, d.Offset, d.LeaseToken, maxAttempts, cause.Error(), backoff(d.Attempts))
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return ErrLeaseLost }   // lease stolen; the new owner will terminate it
	return nil
}
func (l *PgLog[Msg]) DeadLetter(ctx context.Context, d *Delivery, cause error) error {
	const q = `UPDATE deliveries SET state='dead', lease_token=NULL, lease_until=NULL, last_error=$4
	            WHERE consumer_group=$1 AND "offset"=$2 AND lease_token=$3;`
	tag, err := l.Pool.Exec(ctx, q, d.Group, d.Offset, d.LeaseToken, cause.Error())
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return ErrLeaseLost }
	return nil
}

// Advance — slide the waterline to the lowest blocker on this lane: the lowest OPEN LEASE (an
// in-flight range) or the lowest UNRESOLVED exception (ready|inflight). dead rows do NOT block
// (they're resolved); successful offsets left no row. If neither blocks, committed = claimed.
func (l *PgLog[Msg]) Advance(ctx context.Context, group string, lane int) (int64, error) {
	const q = `
		UPDATE cursors c
		SET committed = GREATEST(c.committed, COALESCE(
			LEAST(
				(SELECT min(lo)          FROM leases     WHERE consumer_group=$1 AND lane=$2),
				(SELECT min("offset")-1  FROM deliveries WHERE consumer_group=$1 AND lane=$2
				                          AND state IN ('ready','inflight'))   -- R2 FIX: lane-scoped
			),
			c.claimed))                              -- no blockers: everything reserved is resolved
		WHERE c.consumer_group=$1 AND c.lane=$2
		RETURNING committed;`
	var committed int64
	err := l.Pool.QueryRow(ctx, q, group, lane).Scan(&committed)
	return committed, err
}
```

## Call sites

```go
// HAPPY PATH worker: reclaim-then-claim a range, process, park only the failures. No per-event rows.
func happyWorker(ctx context.Context, log Log[Msg], group string, lane, batch int, lease time.Duration) {
	r, events, reclaimed, _ := log.Reclaim(ctx, group, lease)
	if !reclaimed {
		r, events, _ = log.Claim(ctx, group, lane, batch, lease)
	}
	if r.Lo < 0 || len(events) == 0 { return } // caught up
	var exc []Exception
	for _, e := range events {
		if err := process(ctx, &e); err != nil {
			exc = append(exc, Exception{Offset: e.Offset, Err: err.Error()}) // fell off the happy path
		}
	}
	log.Commit(ctx, r, exc) // successes vanish; failures become deliveries rows; lease released
}

// EXCEPTION worker: per-message retry lifecycle, success = pop-delete.
func exceptionWorker(ctx context.Context, log Log[Msg], group string, n, maxAtt int, lease time.Duration) {
	ds, events, _ := log.ClaimExceptions(ctx, group, n, lease)
	for i := range ds {
		switch err := process(ctx, &events[i]); {
		case err == nil:    log.Ack(ctx, &ds[i])                 // DELETE (pop-delete)
		case terminal(err): log.DeadLetter(ctx, &ds[i], err)     // -> dead (stays as DLQ)
		default:            log.Nack(ctx, maxAtt, &ds[i], err)   // -> ready, backoff
		}
	}
}

// ROLL-UP: lazy ticker (or one advisory-locked roller per lane). Staleness only delays GC.
committed, _ := log.Advance(ctx, group, lane)
```

## Correctness notes

1. **Atomic hand-off keeps the waterline honest.** `Commit` parks failures AND frees the lease in one
   transaction, so every offset in `(lo,hi]` ends up either *resolved on the happy path* (no row) or
   *durably an exception* — never both, never neither. The audit's invariant (`done == head × G`, no
   dup/no skip) was machine-verified for exactly this claim-from-log + waterline structure.
2. **Crash safety (d5).** A crash before `Commit` leaves the lease; `Reclaim` re-reads that exact range
   and reprocesses it (at-least-once → `process` must be idempotent). A crash after `Commit` is safe —
   the exceptions table durably owns those offsets. d4 (no lease) would silently skip the range.
3. **`dead` passes the mark.** `Advance` blocks only on `ready|inflight`; `dead` rows are resolved, so
   `committed` rises past them. They persist below the line as the DLQ (`WHERE state='dead'`). `GREATEST`
   keeps `committed` monotonic under concurrent rollers.
4. **Group caught up** ⇔ `Watermark(group) == head` AND no `ready|inflight` deliveries for the group.

## Scaling one hot group past the single-frontier wall (escape hatch)

Most setups don't need this: with several groups the aggregate already hit 460–770k units/s. But a
**single** group's frontier is one `cursors` row, and concurrent workers contending on it cap it at
~136k. The benchmark fix (measured **+280%**, 136k→521k at K=16): give the group **K lanes**
(`cursors` rows `lane=0..K-1`), each owning a **disjoint, contiguous** block of the log, and define the
group guarantee as `Watermark = min(committed)` over its lanes.

**R1/R4 FIX — block-only, with a FROZEN cap. The draft `Claim` above must be made lane-aware before
sharding is safe:** clamp the frontier to the lane's block (not the global head), and freeze the block
bounds so a growing head can't migrate offsets between lanes. This is exactly what the benchmarked
`claim_log_sharded.sql` does via `cursors.projected`:

```sql
-- assign K disjoint blocks ONCE from a frozen head H; seed each lane at its block floor.
INSERT INTO cursors(consumer_group, lane, committed, claimed, block_hi)
SELECT $g, s, (H*s/K), (H*s/K), CASE WHEN s=K-1 THEN H ELSE H*(s+1)/K END
FROM generate_series(0, K-1) s;

-- lane-aware Claim: cap at the lane's FROZEN block_hi, never the live head.
UPDATE cursors c SET claimed = LEAST(c.claimed + $batch, c.block_hi)
 WHERE c.consumer_group=$g AND c.lane=$lane AND c.claimed < c.block_hi
RETURNING old.claimed AS lo, new.claimed AS hi;
```

With frozen contiguous blocks: lanes never overlap (no dup), never leave a seam (no gap), each `committed`
is a true dense frontier within its block, and `Watermark = min(committed)` is a sound group guarantee
once every lane has drained at least into its block. The `Advance` exception blocker must be lane-scoped
(`AND lane=$lane`, the R2 fix) so one lane's poison message doesn't freeze the others.

> **The `offset % K` "streaming" mode is removed.** A dense single-integer `committed`/`claimed` cursor
> cannot represent a sparse stripe: the draft `Claim` advanced a *contiguous* frontier while a stripe
> worker processed only 1/K of it, silently skipping the rest (test T2). Striping a growing log needs a
> per-stripe dense sequence (e.g. a partitioned/keyed sub-log), which is a different structure — out of
> scope here. For a growing log, prefer more *groups* (which already shard cleanly) over lanes.

Keep `K=1` (single frontier) until one group is provably frontier-bound — sharding trades the clean
single waterline for K sub-waterlines whose min is the guarantee, and requires the frozen-block `Claim`
and lane-scoped `Advance` above.

## Performance expectations (from the audit, 8-core box, tiny payloads)

| path | measured | notes |
|---|---|---|
| happy path (claim-from-log, per group-set) | ~460–770k units/s | no per-event rows; degrades little with log size |
| single hot group, frontier-bound | 136k → 521k with K=16 lanes | the escape hatch above |
| exception drain (pop-delete) | ~258k units/s @ b1000 | only the exceptional fraction flows here |

If ~1% of traffic is exceptional, one pop-delete worker (~258k) keeps up with a happy path running
many× faster — you pay the per-row cost only for the messages that actually need the lifecycle.
