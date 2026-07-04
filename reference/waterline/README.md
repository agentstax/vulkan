# `reference/waterline` — the hybrid managed-cursor platform, in runnable Go

This is a **reference implementation** of the end-state described in
[`LEARNING_PLAN.md`](../../LEARNING_PLAN.md): an immutable append-only **log**
(`events`) plus a sparse per-`(group)` **lifecycle** window (`deliveries`),
reconciled by a lazily-advanced **waterline** cursor. It is the design from
[`bench/scale/waterline_design_v2_hybrid.md`](../../bench/scale/waterline_design_v2_hybrid.md),
turned into compiling, tested Go with the six load-bearing correctness invariants
(R1–R6) implemented inline.

It is deliberately **outside `pkg/`** and imports nothing from it — it is a
self-contained teaching artifact you can read top to bottom and run on its own.
It is a check-your-work companion for the learning plan, **not** a replacement for
building it yourself.

## The shape

```
producer ──► events (immutable log: offset, topic, routing_key, headers, partition_key, payload)
                          │
        happy path        │  CLAIM A RANGE (no per-event rows)
   ┌──────────────────────┴───────────────────────┐
   │ Claim → process → Commit                       │   cursors(committed, claimed, block_hi)
   │   successes vanish (cursor advances)           │   leases(lo,hi,token)  ← crash-safe reclaim
   │   failures park as exceptions ─────────────┐   │
   └────────────────────────────────────────────┼───┘
                                                 ▼
                         deliveries (SPARSE exception window: ready│inflight│dead)
                         drained pop-delete (success = DELETE, no 'acked' state)
                                                 │
                              Advance ───────────┘  slides the waterline to the
                                                    lowest open lease / unresolved
                                                    exception (dead does NOT block)
```

The happy path writes **no per-event rows** — that is the throughput win the
benchmark measured (claim-from-log ran 4–8× the per-row path). Only the offsets
that fall off the happy path get a `deliveries` row.

## Run it

```sh
# 1) throwaway Postgres 18 on :5433 (never touches the dev DB on :5432)
docker run -d --name vulkan-bench-pg --cpus=8 --memory=8g -p 5433:5432 \
  -e POSTGRES_USER=bench -e POSTGRES_PASSWORD=bench -e POSTGRES_DB=bench \
  postgres:18 -c shared_buffers=2GB -c max_wal_size=4GB -c max_connections=200

# 2) create the schema
go run ./reference/waterline/cmd/producer -migrate -count 0

# 3) produce + consume (the LEARNING_PLAN harness knobs work here)
go run ./reference/waterline/cmd/producer -count 100
go run ./reference/waterline/cmd/consumer -group demo --sleep 0.05 --fail-rate 0.1

# routing (Phase 7)
go run ./reference/waterline/cmd/producer -count 1 -routing-key orders.eu.created -headers '{"region":"eu"}'

# FIFO partitions (Phase 12)
go run ./reference/waterline/cmd/producer -count 50 -partition-key acct-42
go run ./reference/waterline/cmd/consumer -group fifo -mode fifo -workers 8

# benchmark vs the SQL targets
go run ./reference/waterline/cmd/bench

# tests (skip automatically if :5433 is down; override with WATERLINE_DSN)
go test ./reference/waterline/
```

## The invariants (R1–R6) and where they live

| # | Invariant | Where | Test |
|---|---|---|---|
| R1/R4 | sharded `Claim` clamps to a **frozen contiguous block** (`block_hi`); no `offset%K` striping | `sharding.go` `InitLanes`, `pglog.go` `Claim` | `TestShardedClaimDisjoint` |
| R2 | `Advance`'s exception blocker is **lane-scoped** | `pglog.go` `Advance` | `TestLaneScopedAdvance` |
| R3 | `Commit` frees the lease **first** (token-guarded), aborts `ErrLeaseLost`, parks nothing if not ours | `pglog.go` `Commit` | `TestStaleCommitNoPhantom` |
| R5 | `Reclaim` = one atomic `FOR UPDATE SKIP LOCKED` + **token rotation** | `pglog.go` `Reclaim` | `TestReclaimSkipLocked`, `TestCrashReclaimReprocess` |
| R6 | `Nack`/`DeadLetter` are token-guarded in-place UPDATEs; park uses `ON CONFLICT DO UPDATE` so maxAttempts→dead always fires | `pglog.go` `Nack`/`DeadLetter`/`Commit` park | `TestDeadUnblocksWaterline` |

Plus the terminal invariants (`done == head`, no gap/dup, 0 dangling leases,
waterline reaches head): `TestHappyPathTerminalInvariants`.

## Feature design choices (the three the learning plan wants)

### Routing (Phase 7) — a predicate at read time, no per-event row
`bindings(consumer_group, kind, …)`: a group with **no** binding matches all
events; `kind='topic'` matches a NATS pattern (`*`/`>`) translated to a POSIX
regex over `routing_key`; `kind='header'` matches `headers @> jsonb`. The
predicate is pushed into the happy-path read (`readRange`), so non-matching
payloads are never transferred — **but the cursor still advances over the whole
contiguous block**, so `committed` stays a dense frontier and a non-matching
offset is "resolved" with no work. Consequence (the Phase 7 question): a binding
added *after* events exist only affects offsets at/above the group's current
frontier; route history by replaying (reset the cursor). See `routing.go`.

### FIFO partitions (Phase 12) — ordering opt-in, on the lifecycle path
The cheap claim-from-log happy path is the **unordered** max-throughput fan-out.
A stream that needs ordering opts into the **lifecycle path**: `Materialize` a
delivery per event (the Phase 6 fan-out), then drain with `ClaimPartitioned`,
which enforces (1) at-most-one-in-flight per non-null key, (2) **FIFO through
retry** — only the *lowest unresolved offset* of a key is eligible, so a
backed-off head blocks its later offsets; a dead head stops blocking. NULL keys
carry no constraint and parallelize fully. Why a separate path: a dense
contiguous cursor cannot selectively defer one key's offset while advancing past
its neighbours, so per-key order lives in the `deliveries` layer (per-row state),
not the frontier-lane layer (contiguous blocks for read throughput — a different
axis). See `partitions.go`.

### Log compaction (Phase 8) — keep latest per key, safely
`Compact` keeps only the latest event per `partition_key` and removes a key whose
latest value is a **tombstone** (`payload IS NULL`); unkeyed events are never
touched. It is safe because the cursor advances by **coordinate** — deleting
events leaves gaps in the offset space, but a gap is just a range that returns
fewer rows, so `Claim`/`Advance` are unaffected. The `floor` bounds how far
compaction may go: `CompactSafe` uses `min(committed)` across all groups (no group
loses a value it had not decided on); `Compact(topic, head)` gives true
compacted-topic semantics (a slow consumer may jump to the latest per key). See
`compaction.go`.

## Benchmark

See [`BENCH_RESULTS.md`](./BENCH_RESULTS.md). Summary: the happy path runs
200k–860k units/s (single-lane → sharded → multi-group), the exceptional pop-
delete path is the ~100k–164k fallback, and per-message ack is the ~4k fsync
wall you escape by batching — reproducing the *shape* of the SQL/pgbench targets,
with the absolute gaps explained.

## How it maps to the learning plan

| Phase | Concept | Reference touch-point |
|---|---|---|
| 1–2 | durable atom, lifecycle (claim/ack/retry/dead) | `deliveries` + `ClaimExceptions`/`Ack`/`Nack`/`DeadLetter` |
| 1.5 | transactional enqueue | `AppendTx` |
| 3 / 3.5 | competing consumers, the commit wall | `ClaimExceptions` + `AckBatch` (batch-ack lever); see `BENCH_RESULTS.md` |
| 4 | log/queue split, replay | `events` + `cursors`; replay = reset `claimed`/`committed` |
| 5 | fan-out | independent `cursors` per group |
| 6 | lifecycle **on** the log | the happy path + sparse `deliveries` exception window |
| 7 | routing | `bindings` + `routing.go` |
| 8 | retention / compaction / observability | `compaction.go`; `Watermark`/`CaughtUp`; lag = `head − committed` |
| 12 | FIFO partitions | `partition_key` + `partitions.go` |
