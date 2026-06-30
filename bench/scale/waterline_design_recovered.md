# Recovered waterline / managed-cursor design (from session 2026-06-21)

> Verbatim recovery of the two original waterline code messages, BEFORE the benchmark/audit findings.

> NOTE: as written, `Fanout`/`Expose` materializes a delivery/cell row for EVERY new offset — the

> per-event-row pattern the audit showed is the consume-side wall. The modification turns the happy

> path into claim-from-log (no per-event rows) and uses delivery/cell rows only for exceptions.


## VERSION A — waterline-themed (Surface/Cell/Rise)  _(transcript line 1899, 2026-06-21T16:21:45.869Z)_

Good call — Waterline it is. The nice property is that the code can *read like the animation*: workers **fill** or **seal** cells, the **level rises** over the settled run, and submerged cells **drain** away. I deliberately avoided the cartoon labels ("air pocket", "surge", "capped") and picked terms that fit the water model *and* read as real engineering vocabulary.

## Naming map (technical → code → waterline meaning)

| Technical concept | Code name | In the picture |
|---|---|---|
| high-water mark / cursor position | `level` (field), `Cursor` (type) | the waterline height |
| immutable log; offset | `events` / `offset` | the vessel; a cell's height in the column |
| per-(group,msg) exception state | `Cell` / `cells` table | a cell in the surface zone above the line |
| exception window / "holes" | the cells with `offset > level` | the messy region above the surface |
| ready / claimable | `pending` | open cell, no water yet |
| in-flight (lease held) | `claimed` | a worker holding that pocket |
| done | `filled` | water poured in |
| dead-letter (retained) | `sealed` | capped so the line rises past it |
| claim | `Claim` | a worker draws open cells |
| ack success | `Fill` | pour water in |
| ack retry | `Defer` | leave the pocket open, try later |
| ack permanent-fail | `Seal` | cap the cell |
| roll-up | `Rise` | the line surges up to the next pocket |
| GC of settled rows | `Drain` | submerged cells discarded (the level represents them) |
| fan-out / projection | `Expose` | inflow that adds new cells at the surface |

## Schema

```sql
-- The vessel: immutable log. offset = a cell's height in the column.
CREATE TABLE events (
    "offset"   BIGSERIAL PRIMARY KEY,
    payload    JSONB        NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- One waterline per consumer group. Everything at/below `level` is submerged (settled)
-- and is represented by this single integer — its cells have been drained.
CREATE TABLE consumer_groups (
    name  TEXT   PRIMARY KEY,
    level BIGINT NOT NULL DEFAULT 0
);

-- The surface zone: per-cell state for offsets ABOVE the level. Sized by the disorder
-- window, not the log. `sealed` rows stay here below the level too — they ARE the
-- dead-letter queue (SELECT ... WHERE state='sealed').
CREATE TYPE cell_state AS ENUM ('pending', 'claimed', 'filled', 'sealed');

CREATE TABLE cells (
    consumer_group TEXT       NOT NULL REFERENCES consumer_groups(name),
    "offset"       BIGINT     NOT NULL,
    state          cell_state NOT NULL DEFAULT 'pending',
    attempts       INT        NOT NULL DEFAULT 0,
    available_at   TIMESTAMPTZ NOT NULL DEFAULT now(),  -- retry/backoff gate
    lease_until    TIMESTAMPTZ,                         -- a worker holds this pocket until here
    lease_token    UUID,
    last_error     TEXT,
    PRIMARY KEY (consumer_group, "offset")
);
```

## Go: the objects

```go
// CellState is a cell's state in the surface zone (above the waterline).
type CellState string

const (
	Pending CellState = "pending" // open — not yet drawn by a worker
	Claimed CellState = "claimed" // a worker holds this pocket (lease)
	Filled  CellState = "filled"  // done — water poured in
	Sealed  CellState = "sealed"  // dead — capped; the line rises past it, kept for audit
)

// Cell is one (group, offset) record in the surface zone. Submerged offsets have NO
// cell — they are represented by the group's Level alone (and were drained).
type Cell struct {
	Group      string
	Offset     int64
	State      CellState
	Attempts   int
	LeaseUntil *time.Time
	LeaseToken pgtype.UUID
	LastError  *string
}

// Cursor is a consumer group's progress over the log: a single rising level. Everything
// at/below Level is settled and compressed to this integer.
type Cursor struct {
	Group string
	Level int64
}
```

## Go: the operations

```go
// Surface manages one group's waterline over the events log.
type Surface[Msg any] interface {
	// Claim draws up to n open (or lease-expired) cells for a worker. Concurrent workers
	// draw DISJOINT cells via SKIP LOCKED; each stamps a short lease.
	Claim(ctx context.Context, group string, n int, lease time.Duration) ([]Cell, error)

	Fill(ctx context.Context, c *Cell) error                                  // ack: done
	Defer(ctx context.Context, maxAttempts int, c *Cell, cause error) error   // ack: retry w/ backoff
	Seal(ctx context.Context, c *Cell, cause error) error                     // ack: permanent fail (kept)

	// Rise advances the waterline over the contiguous settled run and drains submerged
	// filled cells. Lazy, idempotent, OFF THE HOT PATH. Returns the new level.
	Rise(ctx context.Context, group string) (int64, error)

	// Expose materializes pending cells for newly-arrived offsets (fan-out), idempotently.
	Expose(ctx context.Context, group string, upTo int64) (int, error)
}
```

Representative implementations:

```go
// Claim — workers draw disjoint open cells; the lease (not a held lock) tracks in-flight.
func (s *PgSurface[Msg]) Claim(ctx context.Context, group string, n int, lease time.Duration) ([]Cell, error) {
	const q = `
		UPDATE cells
		SET state       = 'claimed',
		    lease_until = now() + make_interval(secs => $3),
		    lease_token = gen_random_uuid(),
		    attempts    = attempts + 1
		WHERE (consumer_group, "offset") IN (
			SELECT consumer_group, "offset" FROM cells
			WHERE consumer_group = $1
			  AND ( (state = 'pending' AND available_at <= now())   -- open cell, ready to draw
			     OR (state = 'claimed' AND lease_until < now()) )    -- a stale pocket to reclaim
			ORDER BY "offset"
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING consumer_group, "offset", state, attempts, lease_until, lease_token, last_error;`
	rows, err := s.Pool.Query(ctx, q, group, n, lease.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[Cell])
}

// Seal — cap a poisoned cell. It stays below the line as the dead-letter record;
// the lease token guards against a stolen lease (RowsAffected==0 => ErrLeaseLost).
func (s *PgSurface[Msg]) Seal(ctx context.Context, c *Cell, cause error) error {
	const q = `
		UPDATE cells SET state='sealed', lease_until=NULL, lease_token=NULL, last_error=$4
		WHERE consumer_group=$1 AND "offset"=$2 AND lease_token=$3;`
	tag, err := s.Pool.Exec(ctx, q, c.Group, c.Offset, c.LeaseToken, cause.Error())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Rise — the waterline surges to the first open pocket, then drains what's submerged.
// (filled and sealed both count as settled; only filled cells are drained — sealed stay
// as the DLQ.) Run lazily; serialize across processes with pg_try_advisory_xact_lock.
func (s *PgSurface[Msg]) Rise(ctx context.Context, group string) (int64, error) {
	const raise = `
		WITH firstPocket AS (
			SELECT min("offset") AS off
			FROM cells
			WHERE consumer_group = $1
			  AND "offset" > (SELECT level FROM consumer_groups WHERE name = $1)
			  AND state IN ('pending','claimed')          -- an open pocket the water can't cross
		)
		UPDATE consumer_groups g
		SET level = COALESCE(
			(SELECT off - 1 FROM firstPocket WHERE off IS NOT NULL),
			(SELECT max("offset") FROM cells WHERE consumer_group = $1)  -- no pockets: rise to the top
		)
		WHERE g.name = $1
		RETURNING level;`
	var level int64
	if err := s.Pool.QueryRow(ctx, raise, group).Scan(&level); err != nil {
		return 0, err
	}
	// Drain submerged filled cells; the level now represents them. Sealed stay for audit.
	const drain = `DELETE FROM cells WHERE consumer_group=$1 AND "offset" <= $2 AND state='filled';`
	if _, err := s.Pool.Exec(ctx, drain, group, level); err != nil {
		return level, err
	}
	return level, nil
}
```

## How it reads at the call site

The hot path is pure water vocabulary, and — exactly matching the model — **workers never touch the level; `Rise` is separate and lazy:**

```go
// worker loop: draw → process → fill / seal / defer. Never advances the level.
cells, _ := surface.Claim(ctx, group, batch, lease)
for i := range cells {
	c := &cells[i]
	switch err := process(ctx, c); {
	case err == nil:       surface.Fill(ctx, c)             // water in
	case terminal(err):    surface.Seal(ctx, c, err)        // poison → capped, line rises past
	default:               surface.Defer(ctx, maxAtt, c, err) // leave open, retry later
	}
}

// elsewhere: a lazy background ticker (or one advisory-locked roller) raises the line.
// staleness is harmless — it only delays draining, never correctness.
level, _ := surface.Rise(ctx, group)
```

## Two correctness notes (so the model stays sound)

1. **`Rise` assumes `Expose` materializes cells contiguously up to the fan-out frontier** — i.e., no gaps below the frontier. Otherwise `level` could rise over an un-exposed offset that was never actually resolved. (`Expose` uses `INSERT … ON CONFLICT (consumer_group,"offset") DO NOTHING` so concurrent rollers/projectors are safe.)
2. **`sealed` rows deliberately persist below the level** — that's your DLQ (`WHERE state='sealed'`). Only `filled` cells drain. When you get to Phase 9 retention, partition `cells` by time and drop old partitions instead of deleting.

This keeps the data structure identical to the managed cursor we worked out (level + exception window + lazy roll-up + dead-passes-the-mark), but every name and verb maps onto something you can *see* in the waterline picture. The water terms are all swappable if any grate — `level`/`Cursor`, `Rise`/`Drain`, `Fill`/`Seal`/`Defer`, `Cell`/`pending·claimed·filled·sealed` are the load-bearing ones.

## VERSION B — canonical CS naming (Log/Delivery/Advance) [recommended base]  _(transcript line 1908, 2026-06-21T16:29:45.366Z)_

Agreed — the waterline is the right *teaching* model, but the code should be named after the canonical lineage this structure actually descends from. Every concept here already has an established CS name:

- **cumulative ack / high-water mark** — Kafka *committed offset*, Pulsar *mark-delete position*, TCP *cumulative ACK*
- **the exception window** — *selective acks* (TCP SACK), *individual acks* (Pulsar), the *in-flight/unacked window*
- **per-consumer-message state** — a *delivery* with a standard lifecycle (ready → in-flight → acked / dead)

Conveniently, this lands almost exactly on the vocabulary your own learning plan already uses (`events` / `cursors` / `deliveries`). Keep the waterline for the docs; use these in the code.

## Naming map (concept → CS name → waterline equivalent)

| Concept | CS code name | Lineage | Waterline (docs only) |
|---|---|---|---|
| cumulative high-water mark | `committed` (offset) | Kafka committed offset / TCP cumulative ACK | the level |
| per-group progress record | `Cursor` / `cursors` | cursor | the gauge |
| exception window record | `Delivery` / `deliveries` | Pulsar/JMS delivery | a cell above the surface |
| claimable | `ready` | job-queue standard | open cell |
| leased / in-flight | `inflight` | SQS "in flight" | pocket a worker holds |
| individually acked (above `committed`) | `acked` | TCP SACK / Pulsar individual ack | filled-but-stranded |
| dead-lettered (retained) | `dead` | DLQ | sealed cell |
| claim a batch | `Claim` | — | draw cells |
| ack success | `Ack` | — | fill |
| negative-ack / retry | `Nack` | AMQP `basic.nack` | leave open, retry |
| terminal fail | `DeadLetter` | DLQ | seal |
| roll-up (fold contiguous acks into `committed`) | `Advance` | mark-delete advance | the level rises |
| GC settled rows | `prune` (inside `Advance`) | log compaction | drain |
| fan-out / projection | `Fanout` | projector | inflow |

## Schema

```sql
-- Immutable log.
CREATE TABLE events (
    "offset"   BIGSERIAL PRIMARY KEY,
    payload    JSONB        NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- One cursor per consumer group. `committed` = cumulative ack: every offset <= committed
-- is fully resolved and compressed to this integer (its deliveries are pruned).
CREATE TABLE cursors (
    consumer_group TEXT   PRIMARY KEY,
    committed      BIGINT NOT NULL DEFAULT 0
);

-- The unacked window: per-(group,offset) delivery state for offsets ABOVE `committed`.
-- `dead` rows persist here below the line too — they ARE the DLQ.
CREATE TYPE delivery_state AS ENUM ('ready', 'inflight', 'acked', 'dead');

CREATE TABLE deliveries (
    consumer_group TEXT           NOT NULL REFERENCES cursors(consumer_group),
    "offset"       BIGINT         NOT NULL,
    state          delivery_state NOT NULL DEFAULT 'ready',
    attempts       INT            NOT NULL DEFAULT 0,
    available_at   TIMESTAMPTZ    NOT NULL DEFAULT now(),  -- retry/backoff gate
    lease_until    TIMESTAMPTZ,
    lease_token    UUID,
    last_error     TEXT,
    PRIMARY KEY (consumer_group, "offset")
);
```

## Structs

```go
type DeliveryState string

const (
	Ready    DeliveryState = "ready"    // claimable
	Inflight DeliveryState = "inflight" // leased to a worker
	Acked    DeliveryState = "acked"    // individually acked, above `committed` (a selective ack)
	Dead     DeliveryState = "dead"     // dead-lettered; retained for audit
)

// Delivery is one (group, offset) record in the unacked window. Offsets at/below the
// cursor's Committed mark have NO delivery row — they're represented by that integer.
type Delivery struct {
	Group      string
	Offset     int64
	State      DeliveryState
	Attempts   int
	LeaseUntil *time.Time
	LeaseToken pgtype.UUID
	LastError  *string
}

// Cursor is a consumer group's cumulative ack over the log.
type Cursor struct {
	Group     string
	Committed int64 // high-water mark: every offset <= Committed is resolved
}
```

## Operations

```go
type Log[Msg any] interface {
	// Claim leases up to n ready (or lease-expired) deliveries; concurrent workers get
	// disjoint rows via SKIP LOCKED.
	Claim(ctx context.Context, group string, n int, lease time.Duration) ([]Delivery, error)

	Ack(ctx context.Context, d *Delivery) error                              // success
	Nack(ctx context.Context, maxAttempts int, d *Delivery, cause error) error // retry w/ backoff
	DeadLetter(ctx context.Context, d *Delivery, cause error) error           // terminal, retained

	// Advance folds the contiguous run of resolved (acked|dead) deliveries into `committed`
	// and prunes the acked rows. Lazy, idempotent, off the hot path. Returns new committed.
	Advance(ctx context.Context, group string) (int64, error)

	// Fanout materializes ready deliveries for new offsets (idempotent).
	Fanout(ctx context.Context, group string, upTo int64) (int, error)
}
```

`Advance` is the only method whose name changed meaning, so here it is concretely:

```go
// Advance — slide the committed offset over the contiguous resolved run, then prune.
// `acked` and `dead` both count as resolved; only `acked` rows are pruned (dead = DLQ).
func (l *PgLog[Msg]) Advance(ctx context.Context, group string) (int64, error) {
	const advance = `
		WITH first_unresolved AS (
			SELECT min("offset") AS off
			FROM deliveries
			WHERE consumer_group = $1
			  AND "offset" > (SELECT committed FROM cursors WHERE consumer_group = $1)
			  AND state IN ('ready','inflight')          -- a gap the cumulative ack can't cross
		)
		UPDATE cursors c
		SET committed = COALESCE(
			(SELECT off - 1 FROM first_unresolved WHERE off IS NOT NULL),
			(SELECT max("offset") FROM deliveries WHERE consumer_group = $1)
		)
		WHERE c.consumer_group = $1
		RETURNING committed;`
	var committed int64
	if err := l.Pool.QueryRow(ctx, advance, group).Scan(&committed); err != nil {
		return 0, err
	}
	const prune = `DELETE FROM deliveries WHERE consumer_group=$1 AND "offset" <= $2 AND state='acked';`
	if _, err := l.Pool.Exec(ctx, prune, group, committed); err != nil {
		return committed, err
	}
	return committed, nil
}
```

## Call site

```go
// hot path: claim → process → ack / nack / dead-letter. Never touches `committed`.
ds, _ := log.Claim(ctx, group, batch, lease)
for i := range ds {
	d := &ds[i]
	switch err := process(ctx, d); {
	case err == nil:    log.Ack(ctx, d)
	case terminal(err): log.DeadLetter(ctx, d, err)   // poison → committed advances past it
	default:            log.Nack(ctx, maxAtt, d, err)
	}
}

// separately, lazily (ticker or one advisory-locked roller):
committed, _ := log.Advance(ctx, group)   // off the hot path; staleness is harmless
```

The structure is byte-for-byte the managed cursor we designed — `committed` + the unacked `deliveries` window + lazy `Advance` + dead-passes-the-mark — now named so a systems engineer reads it as "committed offset + selective acks," with the waterline reserved for the explainer. The load-bearing names if you want to bikeshed: `committed` (vs `position`), `Advance` (vs `commit`/`checkpoint`), and `Ack`/`Nack`/`DeadLetter` for the three terminal transitions.