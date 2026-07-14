# NOTES

The real deliverable of the learning plan. One entry per phase:
*"System X does this by ___, and the tradeoff is ___"* plus the
Explain-it-back answers, written from memory.

---

## Phase 1 — The durable atom: append + atomic claim

**What it does, and the tradeoff:** a Postgres-backed queue makes durability and
exactly-one-worker-gets-each-message fall out of a single transaction:
claim → process → delete → commit. The claim (`SELECT ... FOR UPDATE SKIP LOCKED`)
locks the row until the transaction ends; the `DELETE` removes it; the `COMMIT`
makes both permanent atomically. The tradeoff is that the row is held for the
*entire* duration of processing — a slow handler holds its lock (and a DB
connection) the whole time. That's fine at this scale and is the thing Phase 2
fixes by moving the "I'm working on this" claim out of the DB lock and into row
*data* (`status` + `locked_at`).

**The aha:** a crashed worker needs zero recovery code. The transaction rollback
*is* the recovery — Postgres releases the lock and the row is claimable again.
`SKIP LOCKED` is what lets two workers run the same claim query at the same
instant and get *different* rows instead of one blocking the other.

### Explain it back

**1. Why must the `DELETE` be in the same transaction as the claim? Walk both
orderings if it's separate.**

Because a separate delete is unsafe in *both* possible orderings:
- **Delete after processing (separate tx):** the work finishes, then the delete
  has a network blip / crash before committing → the row is still there → it gets
  claimed and processed *again*. Duplicate work.
- **Delete before processing (separate tx):** the row is gone, then the worker
  crashes mid-process → the work is lost forever and never completed. Worst case.

Same-transaction delete risks neither: either the commit lands (processed AND
deleted) or it doesn't (nothing happened, row still claimable). Atomicity is the
whole durability story of this phase.

**2. A worker is `kill -9`'d mid-process. What does Postgres do, and when is the
row claimable again?**

The connection drops with an open, uncommitted transaction. Postgres treats that
as a failed transaction and rolls it back, which releases the `FOR UPDATE` lock.
The row is claimable again as soon as the rollback completes — another consumer's
`SKIP LOCKED` query will see it on the next poll.

**3. What does `SKIP LOCKED` change about the result set, and why is skipping safe
here when it would normally be a correctness bug?**

It removes already-locked rows from the result set instead of blocking on them. A
locked row is "in process" by another worker, so skipping it is exactly what we
want — skipping prevents double-processing rather than dropping work. (The work
isn't lost; it's just owned by someone else right now.)

### Done

- All labs passed: two-workers-no-collisions, the SKIP LOCKED contrast
  (blocking vs skipping), kill-mid-process, crash-after.
- Batch limit pinned to 1 (avoids Trap T1 — batch poisoning).
- Graceful shutdown: in-flight batch finishes via `context.WithoutCancel` +
  timeout; from the queue's point of view a graceful stop and a crash are the
  same — the tx either committed or it didn't.

### Decisions

- **Table name:** keeping `message_log` (the plan's text says `jobs`). Deferring
  the rename; Phase 4's log/queue split is the deliberate rename moment.

---

## Phase 1.5 — Transactional enqueue (the killer feature)

**What it does, and the tradeoff:** because the queue is a *table in the same
database* as your business data, the job `INSERT` and the business write commit in
**one transaction** — both land or neither does. `AppendMessage` opens the tx,
hands it to the producer callback (`ProducerFunc(ctx, tx)`) so the caller's
business write runs on the *same* tx, then INSERTs into `message_log` and commits.
Any error on either path trips the deferred `Rollback` and unwinds both writes.
This is the one thing Kafka/RabbitMQ structurally cannot offer, because the
transaction boundary doesn't reach a separate system. The tradeoff is coupling:
the queue now lives in (and shares connection/transaction budget with) your
business DB — you can't scale or operate it independently the way an external
broker lets you.

**The aha:** "do the thing, and durably record that follow-up work is needed" is
a single atomic step. There is no window where the business row exists but the job
doesn't, or vice versa — so there's no reconciliation code to write.

### Explain it back

**1. Describe the dual-write problem, why neither ordering is safe, and why
retries don't fix it.**

The dual-write problem is writing to **two separate systems with no shared
transaction** — e.g. commit the business row to Postgres, then publish the event
to an external broker (Kafka/RabbitMQ). There's no safe ordering:
- DB commits, then publish fails → work done, event lost.
- Publish succeeds, then DB rolls back → phantom event for work that never
  happened.

Retries narrow the window for transient faults (a network blip), but can't close
it: the process can die *between* the two writes, and a permanent failure (e.g.
validation rejection on the second write) leaves the first one stranded with
nothing to retry against. The only real fix is to make both writes part of one
transaction — which is exactly what putting the queue *in* Postgres buys you, and
why this phase removes the dual-write entirely.

**2. Why can a consumer never observe a job from an uncommitted producer tx? Which
ACID property does the work?**

**Isolation.** Under read-committed, one transaction's uncommitted writes are
invisible to every other transaction. The producer's INSERT lives in the WAL but
isn't visible in the table to anyone but the producer until `COMMIT`, so the
consumer's claim query simply doesn't see the row. (Atomicity guarantees the
producer's own writes are all-or-nothing; Isolation is what governs what *other*
transactions are allowed to see.)

**3. What is the outbox pattern, and what part of it have you already built?**

The outbox pattern reliably gets events to an *external* system without the
dual-write problem: the business write and an insert into an **outbox table**
happen in one transaction, and a separate **relay** process reads the outbox table
and forwards downstream (to Kafka, Elasticsearch, etc.). What I've already built
is the outbox itself — `message_log` is the outbox table, and the atomic
business-write-beside-the-enqueue is the producer side of the pattern. The only
missing piece is the relay (Phase-9-ish; Debezium/CDC reading the WAL is the
canonical version).

### Done

- Migration `002_users` adds the toy business table.
- Producer API threads `pgx.Tx` into the callback so the business write shares the
  enqueue's transaction (the "insert-only client" / River-style shape).
- Labs passed: forced rollback → neither row exists; commit → both exist and a
  worker claims the job; uncommitted producer tx → consumer can't claim until
  commit.

### Decisions

- **pgx coupling:** `ProducerFunc` takes a concrete `pgx.Tx`, coupling this
  otherwise datastore-agnostic package to pgx. Accepted deliberately (pgx is the
  only backend). If a second backend ever appears, extract a driver-neutral
  Querier interface + adapter — TODO marked at `pkg/producer/producer.go`.

---

## Phase 2 — Per-message lifecycle

**What it does, and the tradeoff:** a message is no longer just present/absent —
it carries a state machine in its own columns (`status`, `attempts`,
`can_run_after`, `locked_at`, `last_error`). Claiming stops deleting and instead
flips `status='ready' → 'processing'` and stamps `locked_at`; on success the row
goes `done`, on failure it either goes back to `ready` with a backoff
(`can_run_after = now() + backoff`) or, once `attempts` hits the max, to `dead`.
The tradeoff is that the DB lock no longer protects work for its whole lifetime —
so reclaiming crashed work is now *my* responsibility (the lease), and delivery
becomes at-least-once instead of exactly-once.

**The aha:** the `FOR UPDATE` lock only spans the fast claim statement now
(milliseconds), not the processing. The durable "I'm working on this" is the
*row data* (`status='processing'` + `locked_at`), not a DB lock. That swap is the
whole phase — it's what lets a 10-minute job run without pinning a transaction
and a connection for 10 minutes. The cost of giving up the auto-releasing lock is
that I had to add lease reclamation, and reclamation is what makes delivery
at-least-once.

### Explain it back

**1. In Phase 1, what held the claim? In Phase 2, what holds it? Why did it have
to change?**

Phase 1: the DB lock (`FOR UPDATE`) held the claim for the entire processing
duration. Phase 2: the row *data* holds it — `status='processing'` plus
`locked_at`. It had to change because a long-running job in Phase 1 keeps a
transaction (and its connection) open for its whole lifecycle; under any real
concurrency that pins a huge number of connections open, which doesn't scale.
Phase 2 takes only a millisecond lock to claim, then relies on the row data +
the claim predicate to know what's "in flight" versus available.

**2. The full state machine, with every transition's trigger.**

- `ready` **→** `processing` — a claim matches the row (`status='ready' AND
  can_run_after <= now()`); the claim sets `locked_at`, `attempts++`, and
  releases the lock.
- `processing` **→** `done` — `consumerFunc` returns nil; `RecordSuccess`.
- `processing` **→** `ready` (retry) — `consumerFunc` returns an error and
  `attempts < maxAttempts`; `RecordFailure` sets `can_run_after = now() +
  backoff(attempts)` and records `last_error`.
- `processing` **→** `dead` — `consumerFunc` errors and `attempts >=
  maxAttempts`; terminal, no more retries (this set is the dead-letter queue,
  queryable as `WHERE status='dead'`).
- `processing` **→** `processing` (reclaim) — the worker crashed and the lease
  expired (`locked_at < now() - stuck_window`); the *next* claim matches the
  stuck row via the OR-branch, re-stamps `locked_at` and `attempts++`. No
  separate reaper process — reclamation is folded into the claim.

*Correction vs my recall:* my original answer listed the happy path + both
failure branches but omitted the `processing → processing` **reclaim** edge (the
transition that defines Phase 2), and didn't spell out that retry means
`→ ready` *plus* a backoff on `can_run_after`.

**3. Why does lease reclamation make delivery at-least-once rather than
exactly-once? What property must the consumerFunc have?**

If a `consumerFunc` runs longer than the lease window, the row looks "stuck" and
another worker reclaims and re-runs it — so the same message can be processed
more than once. Two mitigations, and you want both: keep the lease window
comfortably longer than the work timeout so live workers aren't reclaimed, and
make the `consumerFunc` **idempotent** so a genuine double-delivery (crash after
side effect but before `RecordSuccess`, slow worker past its lease, etc.) is
harmless. The timeout buffer reduces *how often* it happens; idempotency is the
only thing that makes it *correct*.

*Correction vs my recall:* I originally framed it as "long lease **OR**
idempotent." Wrong — they're not alternatives. Idempotency is mandatory; a
longer lease only lowers the *frequency* of double-delivery and never eliminates
it (a crash after the side effect but before `RecordSuccess` re-runs regardless
of lease length).

### Done

- Migration `003_lifecycle` adds the five columns (`status NOT NULL DEFAULT
  'ready'` is the one that actually gates the claim).
- Claim is a single `UPDATE … RETURNING` with `FOR UPDATE SKIP LOCKED` in the
  subquery; reclamation folded in via the `OR (status='processing' AND
  locked_at < now() - buffer)` branch.
- Labs passed: backoff/attempts climb and stubborn messages land in `dead`;
  `dead` set is the DLQ-as-a-query; crash-mid-process is reclaimed and completed
  by another worker; **T2 induced** (sleep > lease → same message processed
  twice) and understood.

### Decisions

- **Reclamation = claim predicate, not a reaper daemon.** An expired lease is
  just another claimable row, so the claim's `WHERE` matches it. Keeps the
  design coordinator-free — every worker is symmetric, no singleton sweeper.
- **Stuck window = `workTimeout + 5s buffer`.** The buffer must exceed the work
  timeout or a worker still legitimately processing gets reclaimed (induces T2 by
  accident). Eventually make the buffer configurable.
- **Config single-owner:** the consumer owns the operational knobs
  (`BatchLimit`, `MaxAttempts`, `WorkTimeout`) and passes them into
  `ProcessMessages`; the datastore constructor takes only connection params
  (matching the producer datastore). Removed the duplicated `PostgresDatastoreConfig`
  that had made `WithBatchLimit`/`WithMaxAttempts` silent no-ops.
- **At-least-once is now the contract.** `consumerFunc` should be idempotent;
  noted in the type doc as "ideally idempotent func."

---

## Phase 3 — Competing consumers & batching

**What it does, and the shape:** one `Prefetch` goroutine batch-claims
`min(batchLimit, freeSlots)` rows into a bounded `PressureQueue`; a `Dispatch`
loop drains it, spawning one goroutine per message gated by `WorkerPoolLimiter`.
Backpressure flows backwards — the prefetcher gates on `WaitForRoom` (replacing
the plan's `CanEnQueue`) so a full buffer stops claiming and we never hit the
`EnQueue` drop path (you can't drop a row you've already leased).

**The aha — buffer depth is two different constraints.** For *durability* the
buffer must stay shallow: every buffered row's `lease_until` is already stamped
and `attempts` already incremented, so a row that dwells past its lease window
gets reclaimed and double-processed (and idle rows burn attempts toward `dead`).
For *throughput* the buffer only needs to be deep enough to mask one claim's
round-trip. These are separate — the shallow rule is a lease-safety constraint,
not a throughput law (see the ceiling work below).

### Explain it back

**1. Why is the partial index so much better than a full index on
`(status, run_at)` for this workload?**

A full `(status, run_at)` index indexes *every* row — including the entire
graveyard of `done`/`dead`. It grows with the queue's whole history and rots
(bloat, cache pressure, vacuum cost) even though we only ever query the tiny
live (`ready`) set. A partial index `WHERE status='ready'` contains only live
rows, so it stays small no matter how much history accumulates, and the claim
scan never touches dead entries. (`status` is also low-cardinality, so it's a
poor leading column for a composite index; the partial predicate drops it
entirely and the index orders purely by the useful key.)

*Correction vs my LEARNING_PLAN answer:* I compared *index vs no index* (bitmap
heap scan vs sequential scan, ~0.05ms vs ~0.215ms at 1000 rows). That's a real
effect but it's the wrong comparison — the question is partial vs *full
composite*, and the point is the graveyard: the partial index excludes it, the
full one carries it forever. **Deeper twist from the ceiling lab:** because the
claim does `ORDER BY id`, neither the `(status, run_at)` index *nor* the
`can_run_after` partial index is actually used — the planner takes the primary
key and filters inline, scanning the whole graveyard (0.057ms → 41.8ms, 730×, at
150k `done` rows). The fix was migration 005: a partial index keyed on `id`
covering both live states, so the ordered scan skips the graveyard.

**2. Batch claiming in Phase 1 had a failure-domain problem. Why doesn't
Phase 3's batching have it?**

Phase 1 claimed *and processed* a batch inside one transaction — all-or-nothing.
One message failing, or any mid-batch error/rollback, took down the whole batch;
unrelated messages shared a single success/failure fate. Phase 3 splits it: the
claim is its own fast transaction (flip `status→processing`, stamp the lease,
`attempts++`), and then each message is processed and acked **individually** —
`RecordSuccess`/`RecordFailure` per row, guarded by a `lease_token` CAS. A
failure is recorded only against that one row (its own backoff/retry/dead-letter).
The batch is now purely a round-trip optimization, not a failure unit — one bad
message can't poison its batch-mates.

*Correction vs my LEARNING_PLAN answer:* I described the connection/lock-holding
scalability problem (Phase 1 pinning a connection per in-flight job). True, but
that's the Phase 2 "what held the claim / why it had to change" answer — not the
failure-domain problem, which is about per-message ack isolation.

**3. What was the measured ceiling, what's the bottleneck, and how would you
tell?**

~20–22k msgs/sec at 64 workers on this box, and the bottleneck is the **ack
path** — one single-row `UPDATE` + commit per message, so each pays a WAL fsync
and a round-trip (`synchronous_commit = on`). Batching commits would lift it but
may not be worth it given upcoming topology changes. How I told: (a) sampling
`pg_stat_activity` wait events; (b) raising `batch` lifted throughput then
plateaued — so *not* supply-bound; (c) at a fixed large batch, throughput scaled
with worker count (8k→20k across 8→64) — so it's the concurrent-commit/ack path.

**4. Why must the in-memory buffer stay shallow? What goes wrong with a deep
buffer that didn't for the scrape queue?**

Every buffered row carries a live lease (`lease_until` stamped, `attempts++` at
claim time). In a deep buffer rows dwell past the lease window, get reclaimed by
another worker, and are double-processed (the reclaim logic bounds the damage but
can't prevent it) — and idle rows burn attempts toward `dead`. The scrape queue
in `examples/simple` had no lease and no durability: losing or redoing ephemeral
work was free, so deep buffering was harmless there. Here a shallow buffer is all
you need — just enough to mask claim-SQL latency and keep workers fed.

### Done (measurements)

- **Index lab:** claim on a 150k-`done` / 50k-`ready` table — pkey scan filtered
  150,001 graveyard rows, 0.057ms (fresh) → 41.840ms (730×). With the 005
  `idx_claim_active (id) WHERE status IN ('ready','processing')` it drops to
  ~0.09ms and throughput on a deep backlog recovered ~4.8k → ~19k/s.
- **Ceiling lab:** `throughput = min(supply(batch), ack_capacity(workers))`.
  Claim cost is sublinear in batch (~3.4µs/row asymptote → single-loop supply
  ~290k/s), so the prefetcher is *not* a hard ceiling; at `batch=100` supply is
  round-trip-capped ~17–18k, right at the ack ceiling. Raising batch reveals the
  ack wall (~20–22k @64w), which scales with workers. Full write-up:
  `phase3-ceiling-report.html`.
- **Group-commit valley:** 1 worker ~1.7k/s, 2 workers ~1.1k/s — added
  contention before group commit amortizes the fsync; recovers from 4 workers up.
- **T1 dissolution:** recorded in Q2 above — per-message ack isolation.
- **Variance proof** (`examples/phase_1/variance`, payload-driven `SleepMs`):
  3 slow (3s) at the front of 6000 fast (5ms), 8 workers. Fast throughput *never
  stalled* while slow held workers — 14/14 250ms buckets active, min 150 fast/
  bucket — and stepped from ~180/bucket (5 free workers) to ~310/bucket the moment
  the 3 slow finished (~3.3s). Wall **6.1s vs 60.7s** at concurrency=1, where the
  lone worker head-of-line-blocked on every slow message (min **0** fast/bucket).

### Outstanding

- `examples/simple` doesn't compile (never ported off the removed `CanEnQueue`),
  so `go build ./...` is red. Deferred intentionally.

### Decisions

- **`min(supply, ack)` is the mental model**, not "single prefetch loop is the
  ceiling" (an earlier wrong conclusion from only measuring `batch=100`).
- **005 index keyed on `id`, covering `ready`+`processing`.** The 004 partial
  indexes (`can_run_after WHERE ready`, `lease_until WHERE processing`) can't
  drive an `id` ordering across the `ready OR processing` predicate, so the claim
  never uses them — re-evaluate whether they earn their keep.
- **`MaxConns` on the datastore** (`pool_max_conns`): the default pool caps at
  `max(4, nCPU)`=10, which is a fake ceiling above ~10 workers.
- **Levers, in order:** keep claims cheap (index) → raise batch+buffer to clear
  supply → add workers for ack capacity → batch the acks → multiple prefetchers
  (only past ~290k/s) → archive `done`/`dead` (Phase 3.5).

---

## Phase 3.5 — Throughput: the commit wall

Phase 3 maxed out concurrency and traced the wall to the **ack path**: one
single-row `UPDATE … WHERE id=$1 AND lease_token=$2` per message via autocommit
`Pool.Exec`, so each ack is its own commit and pays a WAL fsync. The claim is one
commit per *batch* (amortized over `batch=100`), so the ack is the half that
resists amortization. This phase measures the only portable lever that survives
the Phase 4 topology change: **`synchronous_commit`** (whether `COMMIT` blocks on
the fsync, or returns early and lets the WAL writer flush async).

### Done (measurements)

**`on` vs `off`, batch=100, count=20000, best-of-3, fresh-seeded backlog per run
(zero graveyard), Docker postgres:18 on localhost.** Harness: `/tmp/sweep.sh`
driving the Phase 3 `bench` binary.

| workers | `on` (msgs/s) | `off` (msgs/s) | speedup |
|--------:|--------------:|---------------:|:-------:|
| 1  |  1,721.7 | 10,312.0 | **5.99×** |
| 8  |  6,586.2 | 25,035.1 | 3.80× |
| 32 | 18,991.7 | 29,874.8 | 1.57× |
| 64 | 21,423.8 | 27,357.3 | 1.28× |

- **The gap is biggest at conc=1 and shrinks as workers rise** — the opposite of
  the naive "more acks/sec ⇒ bigger win" guess. The mechanism is **group commit**:
  under `on`, many concurrent ack-commits are batched into one physical fsync, so
  high concurrency *already* amortizes the fsync and narrows the gap to `off`. At
  conc=1 there's one commit in flight at a time, every ack pays a full fsync, and
  `off` (which skips it) wins ~6×. This is the same amortization the plan's
  measure-only "batch-ack / group-commit" lever exploits explicitly — Postgres
  does it automatically across *concurrent* committers; batch-ack would do it
  across *one* committer's many rows.
- **Concrete fsync cost (conc=1, the clean signal):** per-ack wall = 581µs (`on`)
  vs 97µs (`off`) ⇒ **~484µs is the fsync wait** on this storage. conc=1 is the
  cleanest reading (one commit at a time, fsync fully exposed vs fully skipped)
  and the lowest-variance row (on 1543–1722, off 10180–10312).
- **Baseline reproduces Phase 3:** `on` @64w = 21.4k/s matches the Phase 3
  ceiling (~20–22k), confirming the harness.
- **`off` stops climbing past ~32w** (29.9k → 27.4k @64w): with the fsync removed
  you hit the *other* Phase 3 walls (round-trips, pool, scheduler), so `off`
  doesn't scale forever — it just moves the ceiling up and shifts it earlier.

### Caveats (so the ratio isn't over-read)

- **High-concurrency variance is large** (on@8: 2280–6586; off@64: 14739–27357).
  Best-of-3/max is the right estimator (noise is one-directional — only slows),
  but the robust finding is the *shape* (gap shrinks with concurrency), not the
  exact peak. conc=1 is the number to trust.
- **Docker storage floor.** Absolute fsync cost depends on the container's volume
  (Docker Desktop VM, not bare NVMe). On faster durable storage the `on` baseline
  rises and the `off` ratio compresses; on slower disk it widens. The 6× at
  conc=1 is this box, not a universal constant.
- `local` not tested: identical to `on` without a synchronous standby (it only
  relaxes the *replica* wait, not the local fsync). On a single node
  {on, local, remote_write, remote_apply} all behave as "wait for local flush";
  only `off` skips the fsync. Measuring `local` would need standing up a streaming
  replica — out of scope for "measure, don't over-build."
- DB knob was set via `ALTER DATABASE example_db SET synchronous_commit=off`
  (pool connections inherit it at connect; session-level `SET` would NOT reach
  the pool) and **RESET back to default `on`** after the sweep — the dev DB is not
  left silently non-durable. Re-flip with the same ALTER to adopt it.

### Why `off` is safe here (the free lunch)

A lost ack on crash → lease expires → reclaim → reprocess, which is exactly the
at-least-once contract already accepted in Phase 2 and covered by idempotency. A
lost claim → row still `ready` → re-claimed. No *new* failure mode; just cashing
in risk already priced. (Not free for a bank ledger: no replay path for a lost
commit ⇒ durability is mandatory ⇒ `on` required.) `off` ≠ `fsync=off`: it risks
only the last few hundred ms of commits on crash, never corruption/inconsistency.

### Crash lab (done) — proves `off` adds no new failure mode

Harness: `examples/phase_1/crashlab` (logs every processed payload id to a file =
the app's durable record of work it believed it did) + `/tmp/crashlab.sh`. Seed
5000 durable rows (CHECKPOINT after seed so the backlog survives), run the app,
**`docker kill` Postgres at ~40% processed (SIGKILL → no graceful fsync)**,
`docker start` (crash recovery), drain the reclaimed backlog. `wal_writer_delay`
widened to 10s so the lost set is large/deterministic instead of a race on the
200ms writer tick — same mechanism, just a bigger window than production's ~200ms.

Identical crash, ~2200 processed before the kill:

| | `off` | `on` (control) |
|---|------:|---------------:|
| acks lost on crash (app-processed but not `done` after recovery) | **899** | **85** |
| ids reprocessed (appear 2+× in app log) | 899 | 85 |
| seeded ids processed ≥1× (no loss) | 5000/5000 | 5000/5000 |
| final `done` | 5000/5000 | 5000/5000 |

- **At-least-once held both ways:** every seeded id ran ≥1×, all 5000 ended
  `done`. The crash caused *reprocessing*, never *loss* — lost ack → `processing`
  row whose lease expired → reclaimed by the next claim → reran.
- **The 85 under `on` is irreducible**, not a durability bug: work that ran but
  hadn't acked at the SIGKILL instant. *Any* at-least-once system reprocesses that
  in-flight set on crash regardless of the knob (bounded by concurrency × work
  latency, not by the flush window).
- **`off` added ~814 *extra* duplicates** (899−85) — the acks it lost that `on`
  would have fsync'd. That's the entire cost of `off`: **more duplicate work on
  crash, not a new class of failure.** Duplicates are exactly what Phase 2's
  at-least-once + idempotency already absorb, so the throughput win (Phase 3.5
  table: up to 6×) is bought with risk already priced. Proven, not asserted.
- Note `off` didn't revert *everything* since the checkpoint (1301 acks survived)
  — WAL buffers got partially write()/fsync'd as they filled. Realistic: the lost
  window is "since the last flush," not "since the last checkpoint."
- Cleanup: both knobs reset to defaults (`synchronous_commit=on`,
  `wal_writer_delay=200ms`); verified via `SHOW`.

### Explain it back

Sharpened from my LEARNING_PLAN answers (originals kept there); corrections noted.

**1. Why is fsync-per-commit the throughput wall, and why is the *ack* (not the
claim) the half hardest to amortize?**

fsync is a costly physical mem→disk flush. The claim is **one commit per batch**
(amortized over ~100 rows); the ack is **one commit per message** via autocommit
`Pool.Exec`, so it can't amortize — that's the hard half. The on/off gap is
biggest at low concurrency (6× @1w) and shrinks at high concurrency (1.3× @64w)
because **group commit** auto-batches *concurrent* committers' fsyncs, so `on`
already amortizes when many commits are in flight.
*Correction:* the knob is `synchronous_commit=off` (defers the fsync to the WAL
writer), NOT `fsync=off` (skips it entirely → risks corruption). Group commit
needs concurrent in-flight commits, so conc=1 (one at a time) gets the biggest
win from `off`.

**2. Why is `off` a free lunch here but not for a bank ledger?**

at-least-once ⇒ duplicates are already possible ⇒ consumers are idempotent ⇒ a
lost commit is harmless because reclaim reruns the work. So relaxing durability
buys throughput against risk already priced in.
*Correction (two):* (a) what `off` loses on crash is the **acked-but-not-yet-
flushed** window — work the app *did* ack but whose commit wasn't durable, so it
*looks* unacked after recovery (crash lab: 899 lost under `off` vs 85 under `on`).
Not "unackd work" generically. (b) The ledger contrast is **no replay path for a
lost commit**, not "exactly-once needs distributed transactions." A queue can
replay (the message is still there + idempotency); a ledger that told the customer
"done" then lost the commit cannot, so durability is mandatory. Deciding question:
*is there a recovery path for a lost commit?*

**3. Which of the four levers survive Phase 4, and why do the rest dissolve/relocate?**

Four levers: **`synchronous_commit`** (survives — a global durability knob, blind
to table layout); **batch-ack** (dissolves — the cursor *is* the ultimate batched
ack: N messages acked by one integer write `position=$last`); **archive terminal
rows** (relocates → Phase 9 retention/partition-drop; an append-only log has no
`done`/`dead` rows to archive; returns for `deliveries` in Phase 6); **claim-
hotspot sharding** (dissolves — each cursor reads its own `offset > position`
range, so no competing claim on a shared hot row; returns when competing claims
return on `deliveries` in Phase 6, formalized as Phase 8's `partition_key`).
*Correction:* I named 3 of 4 and missed **claim-hotspot sharding** (the 4th);
also archive *relocates* rather than purely vanishing.

### Outstanding / deferred

- **batch-ack measurement** — skipped (user's call). It's measure-only anyway; the
  cursor model (Phase 4) is its limit case, so the bridge is understood, not built.

---

## Phase 4 — The log/queue split: retention + replay

**What it does, and the tradeoff:** stop deleting on consume. The data splits in
two: `message_log` (append-only — `id BIGSERIAL`, `payload`, `created_at`; rows are
never mutated or deleted) and `cursors` (`consumer_group`, `position`) — one
high-water mark per group. Claiming stops being an `UPDATE` that mutates row state
and becomes a pure read: `SELECT * FROM message_log WHERE id > position ORDER BY id
LIMIT N`. After processing, the consumer advances its cursor (`MoveCursor`). The
whole Phase 2/3 lifecycle machine — `status`, `attempts`, `lease_*`, `Record*`,
reclaim — falls off the hot path; the migration drops those columns (old lifecycle
migrations parked in `migrations/old/`). The tradeoff: I gain free retention and
replay, but I lose per-row resolution — the cursor is a single integer, so I can no
longer say "message 5 failed but 6,7,8 are done." A failure can only stop the
cursor, not punch a hole in it.

**The aha:** the cursor is a **high-water mark**, and "high-water mark" is
load-bearing — it only works if it advances *monotonically over an ordered log*.
This bare cursor read *is* the claim-from-log happy path the whole platform rides
on. Drop the ordering and the abstraction silently breaks (see the correction
below) — that's the lesson the phase is really teaching.

### Explain it back

**1. What can a cursor not express that per-row status could?**

Per-row lifecycle. With one integer I can't represent "5 failed, 6/7/8 succeeded" —
a hole in the middle. On a failure my only moves are *stop* (leave the cursor before
the bad row and retry it forever — head-of-line block) or *skip* (advance past it
and lose it). Per-row `status`/`attempts`/`dead` could mark exactly that one row
failed while its neighbours finished. That hole is the tension Phases 6–6.5 resolve
with a sparse exception side-table.

**2. Why does replay cost nothing?**

Reading position is decoupled from the data, and the log is append-only, so any
position is valid — replay is just `UPDATE cursors SET position = 0` (or to a
timestamp's offset) and the consumer re-reads history. Phase 1 could never do this:
it *deleted* on consume, so there was no history to replay. Replay is free because I
stopped destroying the thing I'd want to replay.

**3. Crash after processing, before the cursor update?**

On restart the cursor still points before that message, so it's claimed and
processed again → at-least-once. Same contract as Phase 2's lease, now enforced by
*ordering* (process-then-advance) instead of a lease: everything at or below the
cursor is durably done, so the consumerFunc must stay idempotent.

*Correction — the real Phase 4 lesson (caught in review):* my first cut of
`ClaimMessagesV2` had `WHERE id > $1 LIMIT $2` with **no `ORDER BY`**. SQL guarantees
no row order without `ORDER BY`, so `LIMIT` returns an *arbitrary* N rows — and since
`ProcessV2` advances the cursor to each returned `id`, the high-water mark can jump
*past* unread offsets and silently drop them forever (cursor=0, ids 1–5, limit 2
returns {4,5} → cursor=5 → 1,2,3 gone). It passed casual testing only because a small
table happens to get a forward PK index scan — coincidence, not a guarantee. The fix,
and the whole point of the phase, is `ORDER BY id`: a high-water mark is only correct
over an *ordered* claim. (My dead V1 claim already had `ORDER BY id`; V2 had regressed
it.)

### Done

- Migration `001_messages` now defines `message_log(id BIGSERIAL pk, payload jsonb,
  created_at)` — append-only, no status columns. `002_cursors(consumer_group pk,
  position bigint default 0)`. Old lifecycle migrations (`003_lifecycle`, claim
  indexes) moved to `migrations/old/`.
- `ClaimMessagesV2`: one transaction — `SELECT position … FOR UPDATE` (errors loudly
  via `pgx.ErrNoRows` if the group was never registered), then `SELECT * … WHERE id >
  position ORDER BY id LIMIT N`, **drain rows (`CollectRows`) before commit** (a pgx
  conn can't commit while a result set is still streaming — "conn busy"). `ProcessV2`:
  process each message, then `MoveCursor` to its id.
- Lab: a fresh consumer registered at position 0 replays history independently of
  other groups (size `BatchLimit` to cover the log); `git diff phase-3..HEAD` shows
  the lifecycle machine deleted from the hot path and per-row failure resolution lost.
- Build + vet green on `./pkg/...` after the `ORDER BY` fix.

### Decisions

- **Per-message cursor advance (vs once-per-batch to `$last`).** I advance after
  *each* message, not once at the end. Costs N updates per batch but gives a tighter
  at-least-once checkpoint — a crash mid-batch only reprocesses from the last
  committed message, not the whole batch. Correct as long as the batch is ordered
  (which `ORDER BY id` now guarantees). The plan's "UPDATE position = $last" is the
  cheaper variant; I chose granularity over round-trips on purpose.
- **`MoveCursor` left as `SET position = $1` (not `GREATEST`).** With an ordered
  claim + one consumer per group, advances are strictly ascending, so monotonicity
  holds without a guard, and the bare `UPDATE` keeps the `RowsAffected()==0 ⇒
  unregistered group` check working. The `GREATEST(position, $1)` monotonic guard
  becomes necessary once Phase 5 puts concurrent advances on a shared cursor —
  deferred to there.
- **`FOR UPDATE` on the cursor only serializes concurrent *claims*, not the
  process→advance window** (the txn commits before processing). Fine for Phase 4's
  one-consumer-per-cursor model; real cross-consumer exclusion is the lease /
  exception-window work in Phase 6+.
- **Lease machinery kept but dormant.** V1 `ClaimMessages`, `backoff`, `ErrLeaseLost`
  and the commented `Record*` blocks stay as reference for when leases return in
  Phase 6.5 (`backoff` shows as an unused-function lint — intentional). They reference
  dropped columns, so they're parked, not live; delete or revive them at Phase 6.5.

---

## Phase 5 — Fan-out to independent consumers

**What it does, and the tradeoff:** many consumers, each with its own cursor over
the *same* `message_log`, each at its own pace. The schema already supported it —
`cursors` is keyed by `consumer_group`, so two groups are two rows with two
independent `position` values. The work was to *formalize* it: a `-group` flag on
the consumer (`just consume group=…`) so I can run several groups side by side over
one log. `Register` → `UpsertCursor(group)` lazily creates a group's cursor at
`position = 0` (the column default), so a **brand-new group starts at the earliest
retained offset and replays forward** without touching any other group. The "lag"
health metric is `head − position` per group: `max(id) FROM message_log` minus each
cursor (now a `just lag` recipe). There's no real tradeoff here — fan-out is pure
upside *paid for in advance* by Phase 4's decision to retain the log; this phase
just spends it.

The other structural change that made the lab work: `ProcessV2` became `Process`
with a real **poll loop** (a `time.Ticker`, `claim → process → advance` each tick,
`ctx.Done()` to stop). Phase 4 was single-shot — fine for a one-pass replay demo,
useless for "slow consumer A keeps falling behind while B stays current," which
needs both to poll continuously. `Claim` is the extracted per-batch body.

**The aha:** fan-out is *free because of a decision made two chapters earlier*.
Deleting on consume (Phase 1) made the message's processed-state a property of the
*message* — one shared bit, so the first consumer to finish ends it for everyone.
Retaining the log + moving processed-state into a per-consumer *cursor* (Phase 4)
made it a property of the *reader*. Same events, N independent readers. The cursor
being per-consumer is the whole unlock; nothing in Phase 5 is new mechanism, it's
just running the Phase 4 primitive more than once.

### Explain it back

**1. Why is fan-out structurally impossible in the Phase 1–3 design?**

Because consumption *mutates or destroys shared message state*. In Phase 1 consume =
`DELETE`; in Phase 2–3 consume = `UPDATE status='done'` on the one row. Either way
"has this been processed?" is a single bit attached to the message itself, not to
any consumer — a one-to-one mapping. The instant one worker finishes, the row is
gone (or `done`) and every other consumer sees it as handled; there's nowhere to
record that consumer B still hasn't read it. Fan-out needs one-to-many: the log
holds the facts immutably and each consumer carries its *own* position. That's
exactly the Phase 4 split — independent `cursors` rows over an append-only log.

**2. Operational risk of a permanently-slow consumer group once retention (Phase 9)
exists?**

It's consumer lag taken to the failure case: its `position` falls so far behind that
retention deletes log rows *the group hasn't read yet* — the data is dropped out from
under the cursor. On the next read the cursor points below the oldest surviving
offset, so those messages are gone for that group, never processed, with no error at
claim time (the `WHERE id > position` read just returns the surviving tail). This is
Kafka's "consumer fell off the retention window." The defense is operational, not
structural: monitor `head − position` (the `just lag` metric) and alarm before lag
approaches the retention horizon — retention and the slowest consumer's lag are in a
race, and you have to guarantee retention wins by a margin.

### Done

- `-group` flag on the consumer harness + `just consume group=…`; `NewWorkConsumer`
  takes the group through to `Register`/`ClaimMessagesV2`/`MoveCursor`, so each group
  is an independent cursor over the shared `message_log`.
- `Process` is now a poll loop (`time.Ticker` on `PollRate`, `ctx.Done()` to stop);
  `Claim` holds the per-batch claim→process→advance. `ProcessV2` rename is complete,
  no stale refs.
- `just lag` recipe: `head − position` per group — the reproducible health metric the
  lab watches diverge.
- Lab: a new group registered mid-run starts at offset 0 and catches up without
  affecting others; slowing group A with `-processorsleep` makes its lag climb while
  group B stays near 0. Independent consumption confirmed.
- Build + vet green on `./pkg/...` and the Phase 5 harness.

### Decisions

- **Naming drift accepted (again).** The plan's Phase 5 talks `events`/`consumers`;
  I'm still on Phase 4's `message_log`/`cursors`/`position`. Same shapes, different
  names — noted so a future reader maps the plan's terms to mine.
- **One consumer per group in this phase — so `MoveCursor` stays non-monotonic
  (`SET position = $1`), and that's still safe.** Correcting my Phase 4 forecast: I
  said the `GREATEST(position, $1)` guard would be needed "once Phase 5 puts
  concurrent advances on a shared cursor." It doesn't — fan-out is *different groups
  on different cursors*, one consumer each, so advances on any one cursor are still
  strictly ascending. Concurrent advances on a *shared* cursor only appear when
  multiple workers compete *within* a group (the sharded-lanes / claim-from-log work
  in Phase 6.5). That's where `GREATEST` actually lands — deferred to there, not here.
- **Failure semantics unchanged from Phase 4.** A `consumerFunc` error returns up
  through `Claim`→`Process` and stops the whole poll loop (the cursor model has no
  per-row failure resolution). I ran the fan-out lab at `fail-rate=0` to keep the
  focus on independent positions; per-group retry/DLQ is the Phase 6 `deliveries`
  work, not a Phase 5 gap.
- **Poll loop ticks before its first claim.** `time.NewTicker(PollRate)` waits one
  interval before the first tick, so a consumer idles `PollRate` before its first
  read — acceptable for the lab, and the same pattern as the parked V1 `Poll`. If
  first-claim latency ever matters, do an immediate claim before entering the loop.

---

## Phase 6.5a — Claim-from-log: the happy path

**What it does, and the win:** the happy path stops writing a row per message. The
cursor grows from Phase 5's single `position` into two frontiers — `claimed` (the
read frontier) and `committed` (the waterline). One `UPDATE … RETURNING` advances
`claimed` over a contiguous range `(low, high]`, I read exactly that range from
`message_log`, process it, and record success by advancing `committed`. **No
per-message row is written.** Where Phase 6 paid O(N) `deliveries` writes (an INSERT
+ status UPDATEs per message per group), N successes now collapse into advancing two
integers on one `cursors` row.

**The aha:** the write amplification didn't move somewhere cheaper — on the happy
path it *vanished*. The cursor was the happy path all along (it's the Phase 4
primitive); Phase 6's per-row table was the detour. A single integer (`committed`)
now speaks for every offset that just worked, and you only ever pay a row for the
exceptional fraction (6.5c).

### Explain it back

**1. Where did the write amplification go, and what carries "this offset
succeeded"?**

The `committed` waterline carries it: every offset **≤ committed** is in a terminal
state (success-only for now). The amplification didn't relocate — on the happy path
it's *gone*. Phase 6 wrote O(N) `deliveries` rows; now a successful message writes
**no row at all**, and N successes collapse into advancing one integer (`committed`)
on one `cursors` row. O(N) row writes → O(1) integer advance.

**2. What do `claimed` and `committed` mean, and how do they relate in the
single-worker, no-failure happy path?**

Three zones on the log: **≤ committed** = resolved/terminal (success only right
now); **`(committed, claimed]`** = claimed-but-not-yet-resolved (in-flight); **>
claimed** = unclaimed (waiting). `claimed` is the read frontier, advanced atomically
at claim time; `committed` is the waterline. In this happy path the gap is
*transient* — `committed` marches up behind `claimed` message by message and catches
it whenever the consumer drains/idles. The gap only becomes a *persistent* structure
in 6.5b (open leases pin it) and 6.5c (unresolved exceptions pin it).

### Done

- Migration `002_cursors`: `position` → `claimed` + `committed` (both `BIGINT NOT
  NULL DEFAULT 0`). `lane`/`block_hi` deliberately deferred to 6.5d.
- `ClaimMessagesWithCursor`: one txn — `claimed = LEAST(claimed + $batch, MAX(id))`,
  capturing the window via a CTE (`old_values`) joined back in `FROM` so `RETURNING
  old_values.claimed AS low, cursors.claimed AS high` returns `(low, high]` on PG
  <18 too (not relying on PG18's built-in `old`/`new`). Empty result (claimed at
  head, or group missing) ⇒ `pgx.ErrNoRows` ⇒ `nil, nil` (caught up). Then `SELECT *
  FROM message_log WHERE id > low AND id <= high ORDER BY id` — **no per-message row
  written**; drain before commit (the Phase 4 "conn busy" lesson).
- `MoveCursor`: `committed = $1 WHERE committed < $1` — monotonic guard.
- `CursorClaim`: claim the range, then per message unmarshal → process →
  `MoveCursor(message.Id)`.
- `Process` is now a `ConsumerType` switch (`CURSOR` / `LIFECYCLE`); 6.5a is the
  `CURSOR` arm. `WithType` builder added; the example pins `WithType(consumer.CURSOR)`.
- Build + vet green on `./pkg/...` (`backoff` shows as an unused-fn lint —
  intentional, it returns in 6.5c).
- Benchmark intentionally **not run** at this point — the "beat the Phase 6 baseline"
  ratio is unrecorded by choice.

### Decisions

- **Per-message commit (vs once-per-batch to `high`).** Carried over from Phase 4: I
  advance `committed` after *each* message, not once at `high`. Costs N cursor
  updates per batch instead of 1, but gives a tighter at-least-once checkpoint (a
  crash reprocesses from the last committed message, not the whole range). It *does*
  dilute the headline write-amp win — committing once at `high` is the O(1)-per-batch
  ideal — but each update is one in-place HOT update on a single row, still far below
  Phase 6's per-row INSERT. Keeping granularity on purpose; not benchmarking yet, so
  the exact ratio stays unrecorded.
- **The monotonic guard forecasted back in Phase 4/5 has landed.** `MoveCursor` is
  now `… WHERE committed < $1` (the `GREATEST`-equivalent I said would arrive "at
  Phase 6.5"). Caveat it introduces: `RowsAffected()==0` now means *either*
  unregistered group *or* a monotonic no-op (re-commit of an already-passed offset),
  yet the error still reads "no cursor registered". Harmless in the ordered
  single-worker happy path (every `message.Id > committed`); revisit when concurrent
  within-group advances arrive in 6.5b+.
- **`old_values` CTE, not PG18 `old`.** Kept the read-old-value-via-CTE approach but
  renamed it off `old` (so PG18's built-in `old` transition alias doesn't shadow it)
  and joined it via `FROM` so `RETURNING` can see it. Works on PG <18; doesn't depend
  on the 18-only feature.
- **`claimed` advances at claim time, before processing — and there is no lease
  yet.** So a crash after claiming but before committing strands `(committed,
  claimed]`: the next claim reads above `claimed` and skips them. This is the known,
  intended 6.5a hole — crash-safety is exactly what 6.5b's range lease adds. Happy
  path only here.

---

## Phase 6.5b — Lease the range: crash recovery

**What it does, and the win:** the 6.5a happy path had a hole — a worker that
claims a range and crashes *before* finishing strands `(committed, claimed]`: the
next claim reads above `claimed` and skips them, and a naive waterline would sail
right over the gap. 6.5b closes it with a **lease per claimed range** — Phase 2's
visibility timeout, but over a *range* instead of a row. Claim now INSERTs a
`leases(token, consumer_group, low, high, until)` row in the **same transaction**
as the `claimed` advance; a crash leaves an *expired* lease another worker reclaims
and re-reads. No new `deliveries` rows — crash recovery rides entirely on the lease
+ the two cursor frontiers.

**The aha:** the lease is the gap's *owner*. The 6.5a gap `(committed, claimed]`
was anonymous in-flight work; now every offset in it belongs to exactly one lease,
and the waterline can't pass an offset until its lease is freed. Crash-safety,
reclaim, and the waterline pin are the same fact read three ways — "this range is
still owned."

### Explain it back

**1. A worker crashes mid-range — walk the recovery. Why rotate the lease token
instead of just refreshing `lease_until`?**

Worker claims `(lo, hi]` (lease inserted, token T) → crashes before `CommitRange` →
lease just sits there → its `until` passes → on a later poll another worker's
**Reclaim-before-Claim** scans `leases WHERE until < now()`, grabs it
`FOR UPDATE SKIP LOCKED`, and re-reads the exact `(lo, hi]` under a **new** lease
(new token T′). It reprocesses (at-least-once → processing must be idempotent).

Rotating the token defends against the **zombie**: the original worker can resurrect
(GC pause, slow syscall) and call `CommitRange`, which is token-guarded
(`DELETE FROM leases WHERE consumer_group=$1 AND token=$2`). If reclaim had merely
bumped `until` and kept token T, the zombie's commit would match T and free the
**live** lease the reclaimer now holds — double-free, and the waterline would
advance over a range still being processed. With T′, the zombie's `DELETE` hits 0
rows: a harmless no-op. (In this impl reclaim is a DELETE + fresh INSERT, so a new
token is structural, not an extra step.)

**2. What does an open lease do to `committed`, and what breaks if the waterline
passes an in-flight range?**

An open lease **pins `committed` at its `low`**: the advance is `committed =
GREATEST(committed, LEAST(min open-lease low, claimed))`, so the lowest open lease
caps it. The reason is what `committed` *means* — **every offset ≤ `committed` is
terminally resolved.** Let it pass an in-flight range and that promise is a lie: if
the worker then crashes, those offsets were never processed, but everything that
trusts the waterline (compaction/GC, "caught up", the durability guarantee) already
counts them done → **silent loss.** (Reclaim itself doesn't depend on where
`committed` sits — it scans the `leases` table — so the failure is the broken
*guarantee*, not a broken reclaim.)

### Done

- Migration `004_leases`: `leases(token UUID DEFAULT gen_random_uuid(),
  consumer_group, low, high, until)`, PK `(token, consumer_group)`. Lease covers
  `(low, high]`.
- `ClaimMessages` (shared by fresh + reclaim): INSERTs the lease in the **same tx**
  as the range read, `RETURNING *` → `ClaimedRange{Lease, Messages}`. New return
  type replaced the bare `[]MessageRow`.
- `ClaimMessagesWithCursor` = **Reclaim-before-Claim**: `ReclaimWithCursor` first
  (one `DELETE … WHERE token IN (SELECT … WHERE until < now() LIMIT 1 FOR UPDATE
  SKIP LOCKED) RETURNING *`, then re-read the exact range under a fresh lease);
  fall through to `FreshClaimMessagesWithCursor` only when nothing's reclaimable.
  Crashed ranges therefore drain before new frontier work.
- `CommitRange(group, token)`: token-guarded `DELETE FROM leases` — frees a finished
  range, no-ops if the worker was reclaimed. Replaced 6.5a's per-message `MoveCursor`.
- `AdvanceWaterline`: the **lazy roller** (own goroutine `RollWaterline`, ticks on
  `PollRate`, off the hot path). **Two statements** — `SELECT LEAST((SELECT MIN(low)
  FROM leases), claimed)` then `UPDATE … SET committed = GREATEST(committed,
  $target)`. See Decisions for why it can't be one statement.
- `CursorClaim` rewritten: claim a `ClaimedRange`, process the whole range, then
  `CommitRange(token)`; the roller advances `committed` separately. `nil` claim ⇒
  caught up, return.
- Lab `examples/phase_1/reclaimlab`: deterministic, self-verifying — drives the
  datastore directly, "crashes" by never committing a claim, short lease, asserts
  exact range + token rotation + waterline pin + `deliveries` empty. Passes.
- Build + vet green on `./pkg/...` (`backoff` still an intentional unused-fn lint —
  it returns in 6.5c).

### Decisions

- **`AdvanceWaterline` must be two statements, not one — the EPQ snapshot trap.** The
  obvious single `UPDATE cursors SET committed = LEAST((SELECT MIN(low) FROM
  leases), claimed) … RETURNING` is **buggy under concurrency** and was caught by the
  real-consumer run (the deterministic lab missed it — it's single-threaded). The
  roller and a claim race: a claim advances `claimed` and inserts its lease in one
  tx. Under READ COMMITTED the roller's UPDATE blocks on the claim's `cursors` row
  lock; when it proceeds, **EvalPlanQual** re-reads the *target row* (`claimed`) at
  its newest version but runs the `leases` subquery on the statement's **original**
  snapshot — so it sees the new `claimed=10` but **not** the new lease → `LEAST(NULL,
  10) = 10` → `committed` sails past the in-flight range. Fix: read `claimed` + `MIN(low)`
  in **one plain SELECT** (one consistent snapshot, no EPQ), then `UPDATE … GREATEST`
  separately. The target can only lag, never overshoot an open lease; `GREATEST`
  keeps it monotonic. *FOR UPDATE can't save the single-statement form* (an INSERT
  has no FOR UPDATE; you can't lock a not-yet-inserted lease; reads never block
  writers); raising the isolation level converts it to abort-retry (worse under
  contention). 6.5c/6.5d add more blocker terms — keep reading **every** term +
  `claimed` in that same single snapshot.
- **Lease covers `(low, high]` (low-exclusive, high-inclusive)** — same half-open
  convention as the claim read (`id > low AND id <= high`), so a reclaimed range
  re-reads byte-identical to the original claim.
- **Reclaim drains before fresh claim, deliberately.** A crashed range is older work;
  draining it first keeps `committed` moving and bounds how far `claimed` runs ahead
  of an unresolved gap. One reclaim per poll (`LIMIT 1`) is enough — backlog of
  expired leases bleeds down across polls.
- **Poison-batch cap deferred to 6.5c.** A range whose processing *crashes the
  worker* would be reclaimed forever. There's nowhere to quarantine those offsets
  until the exception window exists, so the cap (and the `reclaims` counter) lands in
  6.5c. Not a hole — a known handoff.
- **`MoveCursor` is gone.** 6.5a advanced `committed` per message on the hot path;
  6.5b moves all waterline motion to the lazy roller and frees leases on commit. The
  hot path no longer touches `committed` at all.

---

## Phase 6.5c — The exception window: park only failures

**What it does, and the win:** a failing message no longer drags its whole range
down. `Commit` now takes two slices — `MessageException` (retryable) and
`MessageTerminal` (unrecoverable, e.g. a bad payload) — and after freeing the
range's lease it parks **only those**, one sparse `deliveries` row per failure,
collapsed to `ready | inflight | dead` (no `done`: success is a row that's never
written, or — for a row that already exists — a row that gets deleted). A second,
independent poll loop (`DrainExceptions`) claims parked exceptions and runs them
through the exact same retry/backoff/dead-letter shape Phase 2 had, just scoped to
the tiny failing subset instead of every message. `AdvanceWaterline` grows a second
blocker term so `committed` pins below the lowest unresolved exception the same way
it already pinned below the lowest open lease. Closing the loop from 6.5b: a range
that keeps crashing its worker (poison) now gets quarantined into this same window
instead of being reclaimed forever.

**The aha:** almost nothing here is a new failure-handling primitive — it's two
systems that already existed (the 6.5a/6.5b range-claim happy path, and Phase 2's
per-row retry state machine) being wired together. The kill backstop, the
quarantine cap, and the waterline's second term are all just "how do these two
systems hand off to each other," not new mechanism. The sparse row IS the handoff:
it exists exactly as long as a message needs individual attention, and vanishes the
moment it doesn't.

### Explain it back

**1. Why must `Commit` free the lease *before* parking exceptions (and check it
still owns it)? What does a slow/reclaimed worker inject if it parks first?**

Parking is a plain `INSERT` with no ownership check of its own — nothing about the
statement knows or cares whether the worker calling it still holds the range's
lease. If `Commit` parked first and freed second, a worker that's already been
reclaimed (lease expired, a new owner is re-reading and re-processing the exact
same range under a rotated token) could still successfully write exception rows
for messages in that range — a stale worker injecting phantom failure rows into a
range someone else now owns and may be concurrently resolving differently. Freeing
first collapses "am I still the owner" and "give up ownership" into the same
statement: the token-guarded `DELETE` either matches (still owner, proceed to
park) or matches 0 rows (`ErrLeaseLost`, bail before touching `deliveries` at
all). There's no window between a check and an action for a race to land in,
because the check *is* the action.

**2. Why is there no `done`/`acked` state? When a happy-path message succeeds,
what row changes — and when an *exception* succeeds, what row changes?**

A `deliveries` row's existence is itself the "still needs attention" signal, not a
status value written onto it — so success is definitionally "no row," not a
terminal status. On the happy path (a message that never failed) success writes
nothing at all, the same 6.5a win of zero row writes per success. On the exception
path a row already exists (from the earlier failure), so `RecordExceptionSuccess`
**deletes** it rather than flipping it to some `done` state — the row's only
reason for existing was "needs tracking," and once resolved there's nothing left
to track. Both cases converge on the same rule, they just start from different
places (never-written vs. written-then-removed).

**3. What sits in the gap `(committed, claimed]` now — and why is it *not only*
the failed/in-flight work?**

Three things layered together: ranges under an open lease, offsets covered by an
unresolved `ready`/`inflight` exception, and — easy to miss — every
already-*succeeded* offset sitting **above** the lowest of those two blockers.
`committed` is a single high-water mark, not a bitmap, so it can only certify a
prefix; it has no way to say "everything succeeded except message 47." If message
47 is parked and 48–200 all finished cleanly, `committed` still sits at 46 —
48–200 are done and simply head-of-line-blocked behind 47's unresolved retry, even
though nothing is wrong with them. Quarantine (chunk 8) makes this concrete at
range scale too: once a whole range is dumped into the window, its perfectly-fine
sibling messages sit in the same gap as the one that's actually poison, only
distinguishable once each resolves individually via `ClaimExceptions` +
`RecordExceptionSuccess`/`RecordExceptionFailure`.

### Done

- Schema: `can_run_after` (`deliveries`) + `reclaims` (`leases`) folded directly
  into the existing `003_deliveries`/`004_leases` migrations — no new migration
  file, per this project's no-migration-compat-concerns-yet stance.
- `Commit(group, token, []MessageException, []MessageTerminal)`: frees the lease
  token-guarded first (`ErrLeaseLost` + bail if stale), then parks exceptions as
  `ready` and terminals as `dead` in the same transaction. No `ON CONFLICT` (see
  Decisions).
- `CursorClaim` isolates per-message failures: a bad payload becomes a
  `MessageTerminal`, a `consumerFunc` error becomes a `MessageException`; the range
  always frees regardless of individual outcomes — one bad message no longer fails
  its batch-mates.
- `AdvanceWaterline`'s second blocker term: `LEAST(min open-lease low, min
  unresolved ready/inflight exception's message_id − 1, claimed)`. `dead` rows
  don't block.
- `ClaimExceptions`: the crash-loop kill backstop runs first (dead-letters
  expired-`inflight` rows already at `maxAttempts`, no user code involved — a
  poison exception can't reach `RecordExceptionFailure` to resolve itself, so this
  is the only way it ever leaves the window), then claims `ready` + expired-
  `inflight` → `inflight`, joined to `message_log` for payload.
- `RecordExceptionSuccess` (pop-delete, token-guarded) and
  `RecordExceptionFailure` (exhausted attempts → `dead`, otherwise → `ready` +
  `backoff(attempts)`, token-guarded) — both `ErrLeaseLost`-aware, same as
  `Commit`'s guard.
- `DrainExceptions`: a fourth goroutine in `Consume` (cursor-path only), its own
  poll loop separate from `CursorClaim` so a backed-off exception can't block
  fresh ranges and a stuck range can't block a resolvable exception.
- Poison-batch quarantine, closing the 6.5b handoff: fixed the `reclaims` counter
  (was silently reset to 0 on every reclaim by a delete+insert; now a single
  atomic `UPDATE ... SET reclaims = reclaims + 1`), and past `MaxRangeReclaims` a
  range dumps every message into the exception window as a fresh-budget `ready`
  row and frees its lease for good, instead of being handed out again.
- Lab `examples/phase_1/exceptionlab`: deterministic, drives the datastore
  directly — parks one exception, shows `committed` pin below it while later
  ranges keep claiming/committing past it, resolves it, shows `committed` jump
  straight to `claimed`. `just exception-lab`.
- Build + vet green on `./pkg/...`; both `reclaim-lab` and `exception-lab` pass
  with no regression to 6.5b's crash-recovery guarantees.

### Decisions

- **`MessageException`/`MessageTerminal` as two distinct types, not a bool flag
  or sentinel error on one type.** Mirrors `LifecycleClaim`'s existing
  `RecordTerminal`/`RecordFailure` split rather than inventing a new mechanism —
  avoids "boolean blindness" (a flag whose meaning is opaque without external
  context).
- **No `ON CONFLICT` on the park `INSERT`**, though the plan suggested `ON
  CONFLICT DO UPDATE`. Traced it: a `message_id` belongs to exactly one range
  ever (`claimed` only moves forward), and free-lease-first means only the
  still-owning worker ever reaches the `INSERT` — a collision can't happen in
  this design. A real PK violation now surfaces loudly instead of being silently
  absorbed by defensive SQL that had no actual trigger here.
- **`RecordExceptionSuccess`/`RecordExceptionFailure`, not `Ack`/`Nack`.** Named
  after the codebase's own existing verbs (`RecordSuccess`/`RecordFailure`/
  `RecordTerminal`) instead of borrowed message-queue jargon.
- **`concat()` over `||`** for building `last_error` strings — visually
  confusable with logical `OR` from C-family languages.
- **Reclaim rewritten as one atomic `UPDATE`** (`reclaims + 1`, token rotated,
  `until` refreshed) instead of `DELETE` + `INSERT`. The old shape silently reset
  `reclaims` to 0 on every reclaim — the exact bug this chunk's quarantine cap
  depends on being correct, so it had to be fixed before quarantine could work at
  all.
- **Quarantine reuses the exception window rather than inventing range-level
  dead-lettering.** A poisoned range's messages get the *same* per-message
  retry/backoff/dead-letter treatment as an ordinary `CursorClaim` failure, so
  `AdvanceWaterline` needed zero new logic to pin/unpin around a quarantined
  range — it was already generic over "any unresolved exception."
- **`ExceptionClaim`'s `json.Unmarshal` failure returns the raw error (fatal)
  rather than getting its own retry/dead-letter path.** A parked exception's
  payload already deserialized once, in `CursorClaim`, to reach the window in the
  first place — same immutable `message_log` row — so a failure here can only
  mean an invariant broke elsewhere; better to surface it loudly than build
  unreachable recovery machinery for it.

---

## Phase 7 — Routing

**What it does, and the tradeoff:** producers publish with an attribute
(`routing_key`) instead of addressing a consumer directly; a `bindings` row
lets a `consumer_group` opt into only the events whose `routing_key` matches a
`pattern` — a **true wildcard** (`*` matches any run of characters, any
depth), translated to an anchored POSIX regex. A group with no binding still
receives everything, so every earlier phase's behavior is unchanged by
default. The same predicate is pushed into both existing consume models: the
CURSOR path's `readMessages` excludes a non-match from what's *returned*, but
the cursor still advances over the whole claimed range (`committed` stays a
dense frontier, a non-match is "resolved" with no work, not parked); the
LIFECYCLE path's `FanOut` puts the predicate in its `SELECT ... WHERE`, so a
non-match never gets a `deliveries` row *materialized* at all. The tradeoff:
the true wildcard can't pin an exact hierarchy depth (`orders.*.central1` also
matches the deeper `orders.us.high.central1` — there's no way to say "this
many segments, not more"), traded deliberately for simplicity.

**The aha:** routing needed no new plumbing, just a `WHERE` clause bolted onto
two reads that already existed (`readMessages`, `FanOut`'s `SELECT`). The
producer never learns a consumer exists — it writes one attribute and walks
away — and every consume model absorbs that attribute identically through one
shared predicate string (`bindings.pattern`), not a separate matching engine
per model.

### Explain it back

**1. Where does the routing decision execute, and why there rather than at
claim time or produce time? What changes if a binding is added after events
exist?**

At claim/fan-out time: inside `readMessages`'s `WHERE`, evaluated as part of
the claiming transaction, or inside `FanOut`'s `SELECT`, evaluated whenever
`FanOut` runs — never at produce time. `AppendMessage` writes `routing_key`
and never touches `bindings` at all; a consumer evaluates the predicate
against whatever rows are in `bindings` *right now*, not whatever existed when
the message was written. Consequence: a binding added after a message already
exists still applies to it, as long as that message hasn't been claimed
(CURSOR) or fanned out (LIFECYCLE) yet — verified live in `routinglab`, where
a message published before any binding existed still correctly matched a
binding added afterward. It has zero effect on anything already resolved:
already-`committed` offsets or an already-materialized `deliveries` row don't
get re-evaluated. Routing reach is bounded by what's still unclaimed/
un-fanned-out, not by publish order relative to when the binding was created.

**2. What can a depth-precise selector (NATS-style `*`/`>`) express that a
true wildcard can't — and does this system's routing actually need that?**

NATS-style splits `*` (exactly one dot-delimited token) from `>` (one-or-more
trailing tokens), so `orders.*.created` matches *only* a single token in that
slot — `orders.us.created` yes, `orders.us.central1.created` no (that needs
`>` to absorb the variable-length tail). A true wildcard collapses every `*`
to greedy `.*`, so there's no way to write "exactly this many segments, no
more" — depth becomes unpinnable. Nothing this system currently does depends
on that distinction (no phase needs to tell "this depth" from "any deeper"
apart), so the simpler true wildcard covers every real need so far; the
depth-precise upgrade path is documented and deferred in `TODO.md` rather than
built speculatively.

### Done

- `message_log` gets `routing_key TEXT`, folded directly into its original
  `CREATE TABLE` (`migrations/001_messages`) — not a same-table `ALTER TABLE`.
- New `migrations/005_bindings`: `bindings(consumer_group, pattern, display)`
  + an index on `consumer_group`. No `kind`/`header_match` columns — only one
  matcher style exists, nothing to discriminate between.
- Producer API: `Datastore.AppendMessage`/`WorkProducer.Produce` take a
  `routingKey string`; `""` stores SQL `NULL`, not `''`.
- `pkg/consumer/bindings.go`: `BindTopic`/`ClearBindings` (admin calls, not on
  the `Datastore` interface) + `wildcardToRegex` (`*` → `.*`, literal segments
  `regexp.QuoteMeta`-escaped).
- CURSOR path: `readMessages` gained a `consumerGroup` param and the
  `NOT EXISTS (binding) OR EXISTS (matching binding)` predicate; both call
  sites (`ReclaimWithCursor`, `ClaimMessages`) updated.
- LIFECYCLE path: `FanOut`'s `SELECT` gained the identical predicate in its
  `WHERE`.
- `examples/phase_1/routinglab`: self-seeding (publishes its own messages
  under a run-unique routing-key namespace, so it never collides with
  leftover routing keys from earlier runs) and self-isolating (fast-forwards
  each group's cursor to the current log head first). Proves depth-crossing,
  the retroactive-binding behavior from Q1, the CURSOR path's
  filter-but-still-advance, and the LIFECYCLE path's gate-row-creation —
  three groups, one file. `just routing-lab`.
- Bug found and fixed along the way: `readMessages` was `SELECT *` against
  `message_log`, which grew a `routing_key` column in chunk 1 that
  `MessageRow` has no field for — `pgx.RowToStructByName` errors (doesn't
  silently ignore) on an unmapped column, so the CURSOR read path was
  silently broken from chunk 1 onward until this phase's own live
  verification caught it. Fixed by selecting explicit columns instead of `*`.
- Build + vet green across every touched package; `reclaim-lab`,
  `exception-lab`, and the new `routing-lab` all pass with no regression.

### Decisions

- **Topic/`routing_key` matching only — no header/content matcher.** One
  predicate is enough to learn the hard part (wiring it into both consume
  models); header/content (JSONB containment) is cut, not abandoned — see
  optional Phase 7b.
- **A true wildcard, not NATS-style `*`/`>`.** Simpler to build and reason
  about; the depth-precision gap is a documented, deliberate tradeoff
  (`TODO.md`), not a silent loss.
- **`bindings.kind` dropped entirely**, along with the `CHECK` constraint and
  `header_match` column an earlier draft had — with a single matcher style,
  there's nothing left to discriminate between.
- **`FanOut`'s full-table rescan (no per-group high-water mark) is a known,
  separate limitation**, logged in `TODO.md` rather than fixed here — this
  phase only added the routing predicate to whatever `FanOut` already did;
  giving the LIFECYCLE path its own cursor is a bigger scope decision than
  this phase's job.

## Phase 8a — Retention: partition-drop, and the low-volume hybrid

**What it does, and the tradeoff:** `message_log` is `PARTITION BY RANGE (id)`,
not `created_at`, even though retention is time-based — every hot query
(claim range, `MAX(id)`, the lifecycle join) filters by `id`, none by
`created_at`, so id-partitioning is what the planner can actually prune on.
Retention still works because ids are append-ordered: "old enough to drop" is
decidable per partition just from `id`. A janitor loop (`WorkConsumer.Janitor`,
alongside `RollWaterline`/`Project` in `Consume`) does three things each tick:
create-ahead (unchanged from Phase 6.5b, just moved into the recurring loop),
drop every whole partition whose newest row is past `RetentionTTL`, and sweep
the ttl-expired prefix off whatever partitions survive. The tradeoff: a
low-volume log never fills a partition wide enough to earn a drop, so drop
alone would let expired rows sit forever — the sweep's bounded `DELETE`
covers exactly that gap, and only that gap, since a partition that *did* fill
gets dropped whole before the sweep would ever find much left to delete in it.

**The aha:** drop and sweep don't compete for the same rows — they compete
for the same *gap*. At real volume, a partition ages out and gets dropped
before the sweep ever walks far into it; at low volume, no partition ever
fills, so drop never fires and the sweep is the only mechanism doing work.
Neither one is a fallback for the other so much as each covers exactly the
volume range where the other can't fire.

### Explain it back

**1. Why is partition-drop retention so much cheaper than `DELETE WHERE
created_at < X`? (Think WAL, vacuum, indexes.)**

Every `DELETE` is a transactional write to the WAL that has to be committed
and flushed, plus every index entry for that row has to be cleaned up, plus
the freed page adds pressure on vacuum. A partition drop is `DROP TABLE` — a
catalog operation, no per-row WAL, no index maintenance, no vacuum debt. Just
a disk-level removal of the whole relation.

**2. Retention is time-based — so why partition by `id` and not
`created_at`? What exactly goes wrong at claim time with 365 daily
partitions?**

`message_log` is append-only, so `id` is approximately time-ordered — retention
stays decidable per partition using `id` alone. Partitioning by `created_at`
instead would force the primary key to widen to `(id, created_at)` (Postgres
requires the partition key inside any PK), adding write/delete overhead for
no benefit, since nothing actually queries by `created_at`. Worse, every hot
read (the claim range, `MAX(id)`) filters by `id`, and the planner can only
prune partitions using columns in the `WHERE` — partition by `created_at` and
a claim's `id`-range query can't be pruned at all, so with a year of daily
partitions every claim probes all 365 of them instead of the 1–2 an
id-partitioned claim touches.

**3. The hybrid reintroduces `DELETE` — why doesn't it reintroduce the
problem partition-drop exists to avoid?**

Because the sweep never touches the active, high-volume partition —
`SweepExpiredPartitions` only walks the oldest surviving *non-active*
partition. At high volume, drop consumes whole partitions fast enough that by
the time a partition is old enough to sweep, it's already been dropped whole
— the sweep finds an empty prefix, not a `DELETE` under load. At low volume
there's no whole partition to drop yet, so the `DELETE`'s cost is what's
paying for correctness, and it's cheap exactly because the row count is small
by definition. The two mechanisms cover each other's weak end instead of both
running at once.

**4. What does the drop floor protect, and what precisely happens to a
consumer group when you turn it off and drop past its `committed`? (Kafka's
"consumer fell off the retention window," now in your own system.)**

The floor (`MIN(committed)` across `cursors`) protects a lagging group from
having unprocessed messages deleted out from under it. With it off, nothing
detects the gap — `FreshClaimMessagesWithCursor` advances `claimed` by pure
id arithmetic against `MAX(id)` (`claimed = LEAST(claimed + limit, MAX(id))`),
never checking whether rows still exist in that range. The lease still gets
created for `(low, high]` and `readMessages` still runs its `SELECT`; if the
partition backing that range is gone, the `SELECT` just returns fewer rows,
even zero, with no error. `claimed` and then `committed` both advance past the
hole exactly as they would for a normal batch — the group doesn't "jump
ahead" via any special-cased skip, it was always going to advance on
schedule. The dropped rows just silently never get delivered, and there's no
in-band signal that it happened — only an external one, like the Phase 5 lag
metric going quiet.

### Done

- `message_log` converted to `PARTITION BY RANGE (id)`, folded into its
  original `CREATE TABLE` (`migrations/001_messages`) — the migration also
  creates `message_log_0` so the table is insertable before the janitor ever
  runs. Width is a config knob (`WorkConsumerConfig.PartitionSize`), not
  hardcoded past the migration's own first-partition bound.
- `WorkConsumer.Janitor(ctx)`: a ticker loop matching `RollWaterline`/
  `Project`'s shape, spawned via `errGroup.Go` in `Consume`. Runs
  `EnsureNextPartition` (create-ahead, moved here from being a one-shot in
  `Register`), `DropExpiredPartitions`, `SweepExpiredPartitions` every tick.
  `Register` still calls `EnsureNextPartition` once too, as the cold-start
  guarantee that partition 0 exists before `Janitor`'s first tick.
- New `WorkConsumerConfig` fields: `RetentionTTL time.Duration` (zero
  disables retention entirely — both drop and sweep no-op) and
  `AllowDropPastCommitted bool` (default `false`), the explicit opt-in to
  Kafka's "lagging consumer falls off the retention window" semantics.
- `DropExpiredPartitions(ctx, partitionSize, ttl, allowDropPastCommitted)`
  (`pkg/consumer/datastore.go`): lists surviving partitions via
  `existingPartitions` (`pg_inherits`/`pg_class`, parsed straight in SQL —
  `REPLACE(c.relname, 'message_log_', '')::bigint`, no Go-side regex), judges
  each independently with `continue` (never an early return/break), skips the
  active partition, checks `partitionExpired` (newest row's `created_at`,
  read via `ORDER BY id DESC LIMIT 1` — rides the PK index, no `created_at`
  index needed), then the floor (`cursorFloor` = `MIN(committed)`) unless
  overridden. `dropPartition` deletes the partition's orphaned `deliveries`
  rows and the partition itself in one transaction.
- `SweepExpiredPartitions(ctx, partitionSize, ttl, allowDropPastCommitted,
  batchSize)`: walks every surviving partition independently, deleting its
  ttl-expired prefix in `sweepBatch` calls (`DELETE ... WHERE created_at <
  cutoff AND (floor IS NULL OR id <= floor) ORDER BY id ASC LIMIT batchSize`,
  plus the same batch's orphaned `deliveries` rows, one transaction per
  batch) until a batch returns fewer than `batchSize` rows — so an oversized
  backlog drains over several ticks instead of one giant transaction.
  `allowDropPastCommitted` nils the floor out rather than special-casing the
  SQL.
- Three lab programs under `examples/phase_1/` (`partitionlab`,
  `dropfloorlab`, `sweeplab`; `just partition-lab` / `drop-floor-lab` /
  `sweep-lab`), matching the existing reclaimlab/exceptionlab/routinglab
  pattern — self-contained, deterministic, drive the real datastore methods
  against the dev DB directly. `partitionlab`/`dropfloorlab` swap
  `message_log` to a lab-scale partition width for their own run (drop +
  recreate, same shape the migration leaves it in) and restore the
  migration's exact shape on exit, since `message_log_0`'s real
  1,000,000-row width makes multi-partition demos impractical at lab scale;
  this permanently discards whatever rows were in `message_log` each time
  either lab runs (schema is restored, data is not — safe because no FK ties
  `message_log` to `cursors`/`deliveries`/`leases`). `sweeplab` needs no
  swap, since staying inside one never-rolled partition is exactly the
  condition the sweep exists to cover; it uses `AllowDropPastCommitted=true`
  throughout to stay decoupled from whatever committed state other labs'
  leftover cursor rows happen to be at, since the floor is global across
  every group sharing `message_log`.
- Real bug found and fixed along the way (not originally scoped): `Register`
  called `UpsertCursor` unconditionally for every group. A LIFECYCLE group's
  cursor row never advances `committed` (that's CURSOR-only), so it would sit
  at 0 forever and permanently pin the drop floor, blocking every drop. Fixed
  by gating `UpsertCursor` to `CURSOR` type only.
- Build + vet green across every touched package; all three new labs and the
  pre-existing `reclaim-lab`/`exception-lab`/`routing-lab` pass with no
  regression.

### Decisions

- **Retention is per-log, not per-routing-key — an open question, not
  settled.** One shared `message_log`, one `RetentionTTL`, and the drop
  floor's `MIN(committed)` spans every cursor regardless of what
  `routing_key` it actually consumes, so one lagging group on an unrelated
  topic blocks drops for everyone. Kafka avoids this because `retention.ms`
  is per-topic and each topic is its own log. Logged in `TODO.md`: may need
  an actual topic concept (its own log/partition set) instead of
  `routing_key` filtering over one shared table.
- **No pg_partman, no extensions.** Declarative partitioning is core
  Postgres; pg_partman is only automation around `CREATE TABLE ... PARTITION
  OF` / `DROP TABLE`, and `Janitor` is that automation, in Go, on a ticker.
- **Self-contained DDL swap-and-restore for the labs that need multiple
  lab-scale partitions**, over deferring to a real migration-width config
  knob or skipping the automated pruning proof outright — explicit tradeoff
  accepted: `partition-lab`/`drop-floor-lab` permanently wipe `message_log`'s
  existing rows (schema-only restoration) whenever they run.

## Phase 8b — Per-topic tables: independent logs, routing stays within them

**What it does, and the tradeoff:** two bugs already filed in `TODO.md` turned
out to be the same root cause. 8a's drop floor is `MIN(committed)` across
*every* cursor sharing one `message_log`, so a single lagging group blocks
retention for every unrelated topic riding along in the same table; 8c's
planned compaction lookup would have to probe every live partition up to a
claim's `high`, because `id` was one `BIGSERIAL` shared by every topic,
diluting how densely a rarely-used key's own writes cluster together. Kafka's
own fix is that a topic *is* its own log, not a filter over a shared one —
this phase does the same: each topic gets its own physical table
(`message_log_<id>`), its own dense id sequence, its own partition set, its
own janitor. `cursors`/`deliveries`/`bindings` all gained a `topic_id`
column; `leases` gained the column without a key change, since `token`
already disambiguates a lease on its own. The tradeoff, paid deliberately:
`routing_key`/`bindings` are a *coarser* concept living above topics, not
folded into them — collapsing the two would force a physical table into
existence from a producer's routing-key typo, and would throw away Phase 7's
retroactive binding application (only possible because the log a message
lives in is already shared across every group reading it).

**The aha:** the floor bug and the compaction-cost problem read like two
unrelated performance issues, but they're the same sentence twice — one
shared sequence/log doing the job of many. Once a topic is its own log, both
problems disappear as a structural side effect of the fix, not as two
separate patches bolted onto a shared table.

### Explain it back

**1. Why does each topic need its own dense id sequence rather than sharing
the system-wide one? What specifically breaks if they share it?**

Cursors and partitions. When many topics share one sequence, each topic only
ever occupies a sparse subset of it — conflating what should be a per-topic
concern into a cross-cutting one. Retention is the clearest case: partition
drop decides "expired" by the timestamp of `MAX(id)` in a partition, and
under a shared sequence that max id could belong to any topic. Worse, with
the drop floor enabled, a single lagging consumer forces every topic to wait
on it, because `MIN(committed)` across `cursors` was scoped to the whole
datastore, not to the one topic that consumer actually lags on.

**2. Why do `cursors`/`deliveries`/`leases` need a `topic_id` added to their
keys, when they didn't need one before this phase?**

`leases` technically doesn't need it in its *key* — the lease `token` is
already a unique random id, so it disambiguates a row on its own. But every
table needs the *column* to make what it's tracking unambiguous: `cursors`
needs to know which `message_log_<id>` sequence a group's `claimed`/
`committed` actually refer to; `deliveries` needs it because a bare
`message_id` can point to completely different messages in two different
topics' tables once each has its own sequence.

**3. Why is topic registration explicit, when partition creation
(`EnsureNextPartition`) is allowed to self-heal silently?**

Topic registration creates a durable, lasting resource commitment — it
constructs a physical table and locks in configuration, some of it
immutable. Making that explicit forces a deliberate moment instead of
letting it happen as an incidental side effect of a produce/claim call,
lowering the chance of mistakes or drift. Partitions don't carry the same
risk: their naming is a strictly computed value (`id / partitionSize`), not
something a caller supplies, so there's no equivalent of a topic-name typo
silently forking a whole new resource into existence. Partitions are also an
implementation detail users generally shouldn't have to think about at all,
where a topic name is a first-class thing an application deliberately
chooses.

**4. `routing_key`/`bindings` survive this phase with their matching logic
completely unchanged — so what did splitting into per-topic tables actually
fix, and what did it deliberately leave unfixed?**

It fixed the cross-topic version of both problems named in the What/aha
above: a lagging group's drop floor and a compaction lookup's probe cost are
now bounded by one topic's own volume, not the whole system's. What it
didn't fix, on purpose: retention and partitioning are topic-scoped now, not
per-consumer or per-`routing_key`-slice — two slices sharing *one* topic
still share that topic's one floor. A lagging group reading only
`orders.us.*` still blocks a drop that `orders.eu.*` (same topic, different
slice) would otherwise be free to have happen. Re-scoped from system-wide to
within-one-topic, not eliminated; splitting into separate topics is the
deliberate, manual escape hatch if that ever becomes a real problem.

### Done

- `topic.Config`/`Topic` gained `RetentionTTL`/`AllowDropPastCommitted`,
  folded into `migrations/005_topics.up.sql` — the two remaining log-shape
  knobs that belong on the topic (`PartitionSize` already lived there).
- **Reversed mid-phase, deliberately:** topic identity was first threaded in
  as a `Topic *topic.Topic` field on the consumer/producer datastore structs,
  set once at construction. Reverted in favor of a `topicID int64` parameter
  — placed right after `ctx` — on every `Datastore[Message]` method that
  needs a topic-scoped table name or `WHERE topic_id = $N`, mirroring exactly
  how `consumerGroup` is already passed on every call despite also being
  fixed for a `WorkConsumer`'s whole lifetime. `WorkConsumer`/`WorkProducer`
  themselves keep their own `Topic` field (legitimately fixed per instance,
  same as `Group`) and pass `p.Topic.Id` in at each call site. This also lets
  one datastore instance correctly serve multiple topics, which the
  field-based design structurally couldn't do. Constructors renamed
  `NewPostgresDatastore` → `NewConsumerDatastore`/`NewProducerDatastore`.
- `cursors` (PK → `(consumer_group, topic_id)`), `deliveries` (PK →
  `(consumer_group, topic_id, message_id)`), and `bindings` (`topic_id`
  alongside `consumer_group`/`pattern`, index widened) all gained the
  column, folded into each one's original migration. `leases` gained the
  column without a key change.
- Partition naming became two-part, `message_log_<topic_id>_<n>`, via
  package-level `logTable(topicID)`/`partitionTable(topicID, n)` helpers —
  duplicated once per package (`pkg/consumer`, `pkg/producer/datastore`)
  rather than pulled into a shared package, per house style.
- `cursorFloor` scoped to `WHERE topic_id = $1` — the literal fix for 8a's
  filed TODO. `FanOut`/`ClaimMessagesWithLifecycle` and `Bind`/
  `ClearBindings` all became topic-scoped too, covering the LIFECYCLE path
  and routing end to end.
- Producer datastore (`AppendMessage`) became topic-scoped; the module-level
  `partitionSize` constant is gone, sourced from the topic instead.
- The original global `message_log` table (`migrations/DELETE_001_messages`)
  and the dead, unreferenced V1 consumer datastore package
  (`pkg/consumer/datastore/`) were both deleted outright, by explicit
  decision — nothing left depended on either.
- All 11 pre-existing labs plus the `producer`/`consumer` binaries rewritten
  against the current interface-based, topic-scoped API — they had drifted
  across several earlier phases, not just this one (dead `With*` builder
  methods, a `PostgresConnectionParams` type that no longer exists, methods
  called with signatures years out of date). `reclaimlab`/`exceptionlab`/
  `routinglab` now register and destroy their own disposable topic per run
  instead of depending on shared external state; `partitionlab`/
  `dropfloorlab`'s old DROP-and-recreate-`message_log` schema-swap hack is
  gone entirely, since a lab-scale partition width is now just a
  `PartitionSize` passed to `topic.Register`.
- New `topiclab` (`just topic-lab`) proving 8b's own five-item lab
  checklist end to end: independent per-topic sequences with no
  leak/interleave; a badly-lagging group on topic B provably not blocking a
  drop on topic A; routing/bindings behaving identically to Phase 7 within
  one topic; two `routing_key` slices sharing one topic still sharing its
  floor (the deliberately-unfixed case); an unregistered topic id failing
  with a clean Postgres `42P01 undefined_table`, never silently
  auto-creating one.
- Two real multi-topic bugs found and fixed as a side effect of the
  `topicID`-threading, not originally scoped: `ReclaimWithCursor`'s reclaim
  query and `FreshClaimMessagesWithCursor`'s cursor-advance query both
  lacked `topic_id` filters — without them, two `WorkConsumer`s sharing a
  consumer-group name across two different topics could have
  cross-contaminated each other's reclaim/cursor-advance behavior.
- Build + vet green across the whole repo except `examples/simple/`,
  confirmed pre-existing breakage unrelated to this phase (broke before this
  phase's work started too).

### Decisions

- **`routing_key`/`bindings` are a coarser concept living above topics, not
  folded into them.** Considered and rejected: collapsing `routing_key` into
  topic identity, closer to how a Kafka consumer subscribes by topic
  name-pattern. Rejected because `routing_key` is free text a producer can
  invent with zero ceremony, while a topic carries real weight (its own
  sequence, partition set, retention config) that shouldn't spin into
  existence from a producer typo — and collapsing them would throw away
  Phase 7's retroactive binding application, which only works because the
  log a message lives in is already shared across every group reading it.
- **`deliveries` stays one shared table across every topic, unlike
  `message_log`.** `message_log` needs physical per-topic separation for two
  structural reasons — its own `BIGSERIAL` sequence, and retention-by-
  `DROP TABLE` needing topic-owned partition sets. `deliveries` has neither:
  rows are ephemeral (deleted/resolved continuously, no retention-drop
  mechanism) and aren't keyed by a shared sequence, so a plain `topic_id`
  column plus a wider composite PK is enough — going further to per-topic
  `deliveries` tables would add real DDL-lifecycle cost (every
  `topic.Register`/`Destroy` creating/dropping N more tables) for no
  matching benefit. A `deliveries.status` index was considered for the
  throughput concern this raises at scale and deliberately not added
  preemptively — `status` is touched on nearly every write, so today's
  writes likely already get Postgres's HOT-update fast path, which an index
  would end for every topic's every state transition to speed up a read
  that's only expensive in an already-contained case. Filed in `TODO.md` to
  revisit with real evidence instead of speculatively.
- **Left unfixed, by design: two `routing_key` slices sharing one topic
  still share that topic's one drop floor.** Splitting by topic re-scopes
  the contamination from system-wide to within-one-topic; it doesn't
  eliminate it. If two slices in one topic diverge badly enough in consumer
  lag for that to matter, splitting them into separate topics is the
  deliberate operational escape hatch this phase enables rather than
  automates.

## Phase 8c — Log compaction: latest-per-key, filtered at claim time

**What it does, and the tradeoff:** Kafka's compacted topics keep only the
latest event per key, but get there by appending a new record per write and
deleting older records for that key in the *background*, once a segment ages
out. This phase skips the background step entirely: the log stays
append-only exactly as every earlier phase built it (a write is always a new
`id`, never a mutation), and duplicates get resolved **at claim time**
instead — `readMessages`/`FanOut` only return the row that's currently the
latest for its `compaction_key`. Older rows still physically exist in
`message_log_<topic_id>`, just never selected again once a newer one exists.
This removes the retention floor as a *correctness* requirement — nothing is
ever deleted, so nothing can ever be deleted too early — and downgrades it to
an optional, whenever-convenient disk-space cleanup, fully decoupled from
what a claim is allowed to return.

The correlated "is this the latest for its key" scan is the ground-truth
*definition* of latest, but costs O(partitions since the row) to evaluate
directly — measured, not assumed, at roughly linear growth with no early
termination for a never-superseded key. `latest_keys(topic_id,
compaction_key, latest_id)`, upserted synchronously in the same transaction
as every keyed write, turns that into an O(1) lookup once built — the actual
tradeoff this phase landed on: an O(1) read path, paid for with a second
write on every keyed publish. That write cost was measured too, not just
reasoned about: uncontended, it's noise-level; concentrated on one
screamingly hot key under concurrency, it's a real, quantified 2.5-2.9x
slowdown from lock serialization — a non-issue for the many-distinct-keys
workloads this design targets, a known cost for the pathological one.

**The aha:** the predicate was originally planned as *bounded* — the max
`id` for a key at or below the claim's own `high`, mirroring how a claim's
range is already fixed. Working through the crash/reclaim race in review
showed this was wrong: a lease's `high` is pinned once and reused
identically on every reclaim, so a bounded check re-evaluated against that
frozen window could exclude a row a newer write superseded outside it,
handing that row zero completed delivery attempts instead of at-least-one.
The fix was realizing the guarantee being built was never "every version of
a key gets delivered" — it's "the current latest value gets delivered,
eventually," exactly what Kafka's own compacted topics document. Once that's
the actual target, the predicate can be simply *unbounded* — "nothing with a
higher `id` and the same key exists, anywhere," re-checked live on every
read including reclaims — and CURSOR/LIFECYCLE end up with the *identical*
predicate, not just same-shaped ones.

### Explain it back

**1. Why doesn't this design need a watermark-safe floor to gate
correctness, unlike Kafka's own compacted topics (and this repo's
`reference/waterline/compaction.go`)? What does the floor become instead?**

Because correctness is guaranteed at produce/write time — it's not an async
process that needs an additional correctness gate due to potential lag. The
floor for us is just the standard cursor `committed` value (not `claimed` —
`claimed` can regress on a crash/reclaim, `committed` is the crash-safe
frontier), and it's no longer a correctness gate: it downgrades to an
optional, whenever-convenient disk-space cleanup, decoupled from what a
claim can return.

**2. Why can a brand-new consumer group get latest-per-key on its very
first claim under this design, when a background-delete design can't give
it that for free?**

Because "latest" is guaranteed the moment the producer's transaction
completes, so the claim query always gets the latest id for a
`compaction_key`. A background-delete design has some amount of lag before
compaction is actually complete, so it has no strong guarantee you'll see
the latest — that depends on the size of the background-delete lag.

**3. Why does the filter search unboundedly for a key's latest write
instead of pinning to the claim's own high (`id <= $hi`)?**

Because the guarantee held for a compacted topic isn't "at-least-once per
message," it's "at-least-once per latest `compaction_key`." A bounded check
would only be wrong on reclaim specifically: a lease's `high` is pinned once
and reused on every retry, so after a crash a newer write landing outside
that fixed window would be invisible to a bounded check — the reclaimed row
would look "locally latest" within the stale window even though it's
actually been superseded. Unbounded means the check re-evaluates live
against current state every time, not the state pinned at claim time.

**4. Why is the `compaction_key` index partial (`WHERE compaction_key IS
NOT NULL`) instead of covering every row?**

Because `compaction_key` isn't the standard consumer setup, and a full index
would incur write overhead for no reason. (This index was dropped entirely
later in this same phase, once `latest_keys` made it a dead consumer — this
answers why it *was* partial, not what exists in the final schema.)

**5. Phase 8b split every topic into its own physical table and its own
dense id sequence. Why does that help *this* phase's compaction lookup
specifically — what did a shared, system-wide `BIGSERIAL` cost a single
topic's own key-latest search before 8b existed?**

It doesn't matter for `latest_keys` itself — that lookup is O(1) regardless
of partition count or sequence density by construction. It still matters
for the ground-truth scan underneath it, though: before 8b, proving a
negative meant scanning across every *other* topic's interleaved traffic
sharing the same `BIGSERIAL`, not just this topic's own volume. 8b bounds
that scan to one topic's own history — it just doesn't buy the index
anything, since the index sidesteps the scan entirely.

### Done

- Each topic's `message_log_<id>` gained a `compaction_key TEXT` column
  (`NULL` = not compacted), landed in `createTopicLog` — per-topic tables
  are created dynamically per 8b, not via a static migration file. A
  partial index, `(compaction_key, id) WHERE compaction_key IS NOT NULL`,
  was added alongside it to keep the ground-truth scan's per-partition
  lookup cheap, then dropped later in this same phase once `latest_keys`
  made it a dead consumer (see Decisions).
- Producer: `ProduceOptions{RoutingKey, CompactionKey string}` struct (not
  positional strings, matching this codebase's existing `*Config`
  convention), threaded through `Produce` → `AppendMessage` →
  `appendMessage`'s INSERT.
- `readMessages` (CURSOR, shared by `ReclaimWithCursor` and
  `ClaimMessages`/`FreshClaimMessagesWithCursor`) and `FanOut` (LIFECYCLE)
  originally carried the identical unbounded `NOT EXISTS` predicate, later
  swapped for the `latest_keys` lookup (see Decisions) — the two paths
  still carry an identical predicate, just a cheaper one.
- No schema-level tombstone concept — "how do I delete a key" is answered
  entirely by what the producer puts in `payload` (e.g. an app-defined
  `Deleted bool` field), not by anything this framework provides.
- **`latest_keys(topic_id, compaction_key, latest_id)`** — new migration
  `006_latest_keys`, a single table shared across every topic (scales with
  distinct-key count, not message volume, matching `cursors`/`deliveries`/
  `bindings`'s post-8b shape rather than `message_log`'s per-topic one).
  Write path: `appendMessage`'s INSERT gained `RETURNING id`; a keyed
  publish follows it with `INSERT ... ON CONFLICT (topic_id,
  compaction_key) DO UPDATE SET latest_id = EXCLUDED.latest_id WHERE
  latest_keys.latest_id < EXCLUDED.latest_id` in the same transaction — the
  guard compares by id *value*, not commit order, since `BIGSERIAL`
  allocates at INSERT time not commit time and concurrent same-key
  publishes can commit out of order under READ COMMITTED. Read path:
  `readMessages`/`FanOut` swapped to `m.compaction_key IS NULL OR m.id =
  (SELECT latest_id FROM latest_keys WHERE topic_id = $N AND
  compaction_key = m.compaction_key)` — O(1) regardless of partition count,
  the old unbounded scan deleted outright, no fallback path.
- **Retention (8a) janitor cleanup.** `dropPartition`/`sweepBatch` both
  garbage-collect the now-dangling `latest_keys` row when they reap a
  dormant key's last surviving version, mirroring each function's existing
  `deliveries`-cleanup precedent exactly (range-based delete for
  `dropPartition`, `ANY(ids)`-based for `sweepBatch`). No new gate needed
  in either function itself (see Decisions).
- **A backfill chunk (`BackfillLatestKeys`) was built, verified live, then
  reverted** — this project has no live deployment with compacted-topic
  history predating `latest_keys` to migrate, so it was solving a problem
  that doesn't exist here (see Decisions).
- Six labs: `compactionlab` (`just compaction-lab`) proves latest-per-key
  survives a claim while older rows stay physically present, a delivered
  version stays delivered once superseded, the crash/reclaim race directly,
  a tombstoned key still delivers on both paths, and unkeyed traffic pays
  zero compaction query cost. `compactionwidthlab`/`compactionscalelab`
  (`just compaction-width-lab`/`compaction-scale-lab`) measure the OLD
  scan's cost as a frozen historical due-diligence record — linear growth,
  ~10µs/partition, no amortization, extrapolated to ~1s/surviving-key on a
  100K-partition topic's backlog replay. `latestkeysracelab` (`just
  latest-keys-race-lab`) proves the write-path guard converges under real
  concurrent same-key races and re-runs the scale curve against the NEW
  lookup, flat at every checkpoint. `latestkeysretentionlab` (`just
  latest-keys-retention-lab`) proves both janitor paths correctly expire a
  dormant key's `latest_keys` row while a key touched inside the TTL
  window survives. `latestkeyswritelab` (`just latest-keys-write-lab`)
  quantifies the write-side tradeoff (see What it does above).

### Decisions

- **No schema-level tombstone, considered and rejected.** Kafka's reason for
  a protocol-level tombstone marker is so its background compactor — which
  physically deletes rows — can recognize a deletion generically across any
  topic without understanding that topic's payload schema, and eventually
  purge the tombstone itself. This phase never physically deletes anything,
  so that motivating reason doesn't apply; the filter query already returns
  "whatever the latest row is" with zero special-casing regardless of what's
  inside it. A future generic disk-space cleanup pass would lose the ability
  to recognize "this key is fully dead, purge every row for it" without
  understanding each topic's schema — accepted as a real but currently
  unneeded cost.
- **The predicate is unbounded, not bounded by the claim's own high** — see
  the aha and Explain-it-back Q3 above for the crash/reclaim race that
  forced this. `FanOut` was never bounded by a claim high to begin with (it
  already scans current state each call), so this ended up a better fit for
  it than a bounded version would have been, and makes the two paths'
  predicates exactly identical rather than merely same-shaped.
- **`latest_keys` was sized before it was built, not assumed necessary.**
  `compactionwidthlab` showed the shape (a never-superseded key's
  negative-proof touches every partition; a just-superseded key's
  positive-proof benefits from runtime partition pruning and early
  termination); `compactionscalelab` showed the growth curve is linear with
  no amortization, extrapolating to roughly a second per surviving key on a
  ~100K-partition topic's backlog replay — real due-diligence evidence, not
  a guess, before committing to the schema/write-path/read-path chunk
  sequence that built the index.
- **Retention (8a) needs no compaction-awareness — a dormant key's last
  surviving row aging out is intentional expiration, not a bug.** Matches
  the mental model anyone combining `RetentionTTL` with compaction already
  has, and what every other TTL'd key-value system already does (Kafka's
  own `cleanup.policy=compact,delete`, DynamoDB TTL): "retention" means "a
  key untouched longer than the TTL window ages out," not "keep this
  forever, delete everything else." Falls out for free from an existing
  invariant — id order tracks time order, and compaction always keeps the
  highest-id row for a key, so a key touched inside the TTL window keeps
  landing in fresh partitions immune to both retention gates. Adding a
  compaction-aware gate to `dropPartition`/`sweepBatch` was considered and
  rejected — it would make `RetentionTTL` mean two different things
  depending on whether a message carries a `compaction_key`. The one real
  gap was `latest_keys` cleanup (not prevention), handled by extending each
  janitor function's existing `deliveries`-cleanup pattern.
- **Backfill built, verified, then reverted — no compatibility requirement
  to conserve.** A per-topic backfill (`INSERT INTO latest_keys ... SELECT
  ..., MAX(id) ... GROUP BY compaction_key ON CONFLICT DO NOTHING`) was
  implemented and proven correct live (reconstructs history, idempotent,
  never clobbers a live write mid-race) before being reverted outright:
  this project has no live deployment with compacted-topic history
  predating `latest_keys`, so a migration mechanism for one was designing
  for a user that doesn't exist. Consequence: the read path's
  fallback-vs-require-backfill sub-decision became moot — `latest_keys` is
  authoritative from the moment the write path lands, no fallback, old
  scan deleted outright.
- **The `compaction_key` partial index was dropped once `latest_keys` made
  it a dead consumer.** Verified via `EXPLAIN` (twice — a keyed row
  present, and a pure-unkeyed range matching `compactionlab`'s own shape)
  that the index doesn't appear in the new predicate's plan at all, not
  even as a rejected candidate, and that the unkeyed-row short-circuit
  holds identically against `latest_keys` instead. Caught a real staleness
  bug along the way: `compactionlab`'s own `EXPLAIN` check had silently
  drifted to testing the OLD query after the read-path swap, its comment
  still claiming to test "the exact shape `readMessages` runs" — fixed to
  test the real current predicate before deciding to drop the index. Its
  only remaining consumers, `compactionwidthlab`/`compactionscalelab`, are
  deliberately frozen historical measurements whose job was already done;
  losing their ability to exactly reproduce those recorded numbers on a
  future re-run was an acceptable, not a blocking, cost.
- **The write-side tradeoff was measured, not just flagged.**
  `latestkeyswritelab`: sequential/uncontended publishes showed no
  measurable fixed cost (unkeyed vs. keyed within ±10-20% noise, no
  consistent direction). 50 concurrent goroutines each to their own key vs.
  all 50 hammering one key showed the real cost — 2.5-2.9x slower under
  full serialization on the `ON CONFLICT DO UPDATE` row lock, reproduced
  across repeated runs. A ~1,000-update hot-key burst left ~500 dead tuples
  pending autovacuum — real but *bounded* bloat, since the table holds
  exactly one row per (topic, key) regardless of republish count. A
  non-issue for the many-distinct-keys workloads this design targets; a
  known, quantified cost for a workload that concentrates writes on one
  screamingly hot key.
- **Two side findings, filed rather than fixed here.** Running
  `compactionscalelab` at 2000 partitions made `topic.Destroy` fail with
  Postgres's "out of shared memory" — dropping a partitioned parent needs an
  `ACCESS EXCLUSIVE` lock on every partition and every object each one owns
  (5 lockable relations per partition on this schema), and Postgres's lock
  table is fixed-size at server start. Not specific to compaction — any
  topic that accumulates enough partitions hits this on `Destroy`. Filed in
  `TODO.md` to reimplement `Destroy` with the same batched-drop shape 8a's
  `dropPartition`/`sweepBatch` already use. That, plus the read-cost finding
  above, also motivated a second `TODO.md` entry: a built-in, overridable
  "default alerts" concept for surfacing this kind of silent-until-it-
  happens operational cliff before a user hits it blind. Separately: the
  pre-existing `DeleteTopic` gap (doesn't clean up `cursors`/`deliveries`/
  `bindings`/`leases` on topic destroy) was confirmed to apply identically
  to `latest_keys` — not a new problem, same gap, one more table.

## Phase 9 — Consumer fault isolation & recovery

**What it does, and the tradeoff:** before this phase, only one of
`consumerFunc`'s four possible failure shapes was handled — an ordinary
returned error, routed through 6.5c's retry/backoff/dead-letter exception
window. A panic, a hang, or a datastore blip mid-claim each took down more
than the one message: a panic crashed the whole claimed range (lease
expires, gets reclaimed, re-reads the identical range, panics again,
forever); a hang blocked the batch with no way to give up since Go has no
goroutine kill; a transient DB error propagated straight up through
`Process`'s poll loop and killed the consumer outright. This phase closes
all three by making each one land in the *same* place the ordinary-error
path already does — one message dead-lettered or retried, never the whole
range — rather than inventing a bespoke recovery mechanism per failure
shape. The unifying move is `callSafely`, one shared helper all three claim
paths (`CursorClaim`/`ExceptionClaim`/`LifecycleClaim`) now call instead of
invoking `consumerFunc` raw: it recovers a panic into an ordinary error, and
races `consumerFunc` in its own goroutine against a hard timeout so a call
that never checks its context still returns control to the caller. Go's
lack of a goroutine-kill primitive is the one failure mode this phase only
*contains* rather than fully fixes — an abandoned goroutine keeps running
in the background, tracked (not eliminated) via a small in-process
registry.

**The aha:** the DB-blip fault-isolation lab was written to *confirm* an
already-working guarantee, not find a new bug — and found one anyway, in
code none of this phase touched. `pkg/retry.Retry.Wrap` correctly
classified a nil error as non-retryable, but conflated "not retryable" with
"permanent failure, return the joined error": a call that succeeded after
N retries still returned `errors.Join(err1, ..., nil)`, and `errors.Join`
only discards the nil element — it does not collapse the whole result to
nil. Every caller's `if err != nil { return err }` (every call site in
`producer`/`consumer`/`topic`) would treat a successful retry-then-recover
as a hard failure, which for `CursorClaim` specifically meant a DB blip
that cleared on retry still killed `Process`'s poll loop — the exact
opposite of the guarantee this phase's own "Datastore-blip retry backoff"
bullet had already shipped. The lab exists precisely so a claim like that
gets exercised end-to-end instead of trusted on inspection.

### Explain it back

**1. Why does a recovered panic have to go through the *exact same*
retry/backoff/dead path as an ordinary error, instead of its own
special-cased handling?**

Answer: recovered panics are not necessarily permanent errors (nil map
write, index out of range, bad type assertion). The fact is we don't know
if a retry will help or not b/c we don't know the consumerFuncs code. So it
is better to go on side of caution and follow standard expected path
instead of making assumptions

**2. Why is `context.WithTimeout` alone insufficient to enforce
`WorkTimeout`, and what does the detached-goroutine race actually buy you
given Go has no goroutine kill?**

Answer: context timeouts expect to be explicitily handled. Normally via a
call to ctx.Err or ctx.Done. Our own internal code we can do that for.
However we cannot gauruntee that the user does that within their
consumerFunc. Because of that we have a detached-goroutine race that allows
us to internally exit the consumerFunc work such that we may retry or mark
dead within the users expected WorkTimeout + Grace period. The one caveat
this brings is that the goroutine that was raced is still running and as
such we have a abanonded routine which we track via metrics

**3. Why key the abandoned-goroutine registry by (message, attempt) rather
than by message alone?**

Answer: If first and second attempt of a message was abandoned. The second
attempt would overwrite the first within registry despite their
potentially being two real live abandoned go routines. message & attempt
is the uniquness identifier for the goroutine and as such should be the key

### Done

- **Datastore-blip retry backoff.** New `pkg/retry` package —
  `Retry`/`DatastoreRetry`, explicit `RetryableError`/`PermanentError`
  classification, context-aware exponential backoff. Every
  `Datastore[Message]` method got a public/private split so retry plumbing
  stays invisible at call sites. Surfaced a correctness gap beyond "just
  retry": a retried `AppendMessage` could double-publish on an
  ambiguous-commit-ack. Fixed with an `idempotency_keys` claim table
  (`ON CONFLICT DO NOTHING`, checked before the `message_log` insert),
  swept on `Topic.IdempotencyKeyTTL` (defaults 24h, no "0 = forever").
  Measured the naive version's cost (20-36%+ extra disk,
  15-30% lower throughput) and cut it with a batched data-modifying CTE
  (one round trip instead of two) plus a per-call `SkipIdempotency`
  opt-out. `SkipIdempotency` forced a deeper fix: retry-safety now depends
  on caller context, not just error shape, so `Commit()` classification
  moved inline at each package's own call site
  (`classifyTransient`/`classifyCommit`-shaped `if`/`else`), with
  `DatastoreRetry.Wrap`'s default classifier changed to pass an
  already-classified error through untouched instead of re-deciding it —
  the one shared `Wrap` that lets every package coexist with its own
  inline `Commit()` classification.
- **Graceful-shutdown lease truncation.** `CursorClaim`'s per-message loop
  checks `ctx.Done()` between messages (not mid-message — that's the hard
  per-message timeout, a separate mechanism below). A new datastore method,
  `PartialCommit`, narrows the interrupted lease's `low` bound (`UPDATE
  leases SET low = $lastProcessed`) instead of freeing it outright —
  same-token, same-`until`, so the untouched suffix rides the existing
  crash-recovery reclaim path rather than needing a new expiry mechanism.
  Written through `context.WithTimeout(context.WithoutCancel(ctx),
  AckMargin)`, the same "extra time to record outcomes" pattern
  `ShutdownFunc` already uses, since the ctx that triggered the
  interruption is already `Done`.
- **`recover()` around `consumerFunc`.** `callSafely(ctx, consumerFunc,
  work, messageID, attempt) error` — one shared helper all three claim
  paths call instead of tripling defer/recover logic. A recovered panic
  becomes an ordinary error (panic value + `runtime/debug.Stack()`) routed
  through the same per-message path as any other failure. Does not catch
  OS-level faults (stack overflow, SIGSEGV via cgo, OOM-kill, external
  kill) — those still need 6.5c's range-level quarantine as the backstop.
- **Hard per-message timeout via a detached-goroutine race.** Built into
  `callSafely`: `consumerFunc` now runs in its own goroutine, racing a
  buffered `done` channel against `time.After(WorkTimeout +
  WorkTimeoutGrace)` so the caller can give up independent of the callee.
  `WorkTimeoutGrace` (new config field, default 100ms, validated `> 0`,
  folded into the `ShutdownTimeout` inequality) is sized from a real
  measurement, not a guess: Go's own scheduler wakeup latency from a
  context deadline to a blocked goroutine's channel send is p99 < 1ms even
  under artificial scheduler pressure (2000 trials) — almost none of the
  default 100ms is scheduling risk, it's discretionary slack for the
  caller's own cancellation-response time (e.g. a `pgx` cancel-request
  round trip), sized to roughly one same-region network hop. Timeout
  errors tag `last_error` with `messageID`, never the raw `work` payload —
  `last_error` is DB-persisted with far wider read access than an
  in-process error ever had.
- **Track abandoned goroutines.** `ConsumerMetrics.AbandonedRoutines` — a
  `map[abandonedKey]time.Time` keyed by `(MessageId, Attempt)`, guarded by
  a `sync.Mutex` (not `sync.Map` — the access pattern is one
  Add/Remove/CurrentTotal call each per event, not enough concurrent
  contention to justify `sync.Map`'s striped-lock complexity).
  `TrackingTotal()` (atomic counter), `CurrentTotal()` (gauge, mutex-read
  map length), `ReclaimLatency()` (average over a
  `ConcurrentBoundedRingBuffer[time.Duration]`, capacity 256 — a
  self-synchronizing generic ring buffer with its own internal mutex,
  `Add`/`Len`/`Values` all self-locking, `Values()` returning a defensive
  copy). Deliberately informal (no metrics-library commitment yet) — Phase
  10's "Operational metrics" bullet wires these already-built numbers into
  whatever pluggable interface it settles on.
- **Five labs:** `idempotency-keys-lab` (retry-safety mechanism: exactly-once
  under a retried key, distinct keys never collide, sweep drains expired
  claims, `SkipIdempotency` genuinely double-publishes), `idempotency
  -keys-growth-lab` (storage/throughput axis — 20-36%+ overhead with no
  sweep running, bounded steady-state size with the real janitor cadence),
  `delete-topic-lab` (`Destroy` cascades all six topic-scoped tables, not
  just `message_log`/`topics`), `shutdown-truncation-lab` (a mid-range
  cancellation narrows the lease, never redelivers the resolved prefix, the
  waterline's exception-blocker and lease-narrowing terms combine via
  `LEAST`), `fault-isolation-lab` (all three induced failure modes through
  the real `CursorClaim` path — panic and hang each isolate to one message
  in a 3-message batch, the abandoned-goroutine gauge shows exactly 1
  outstanding and self-clears, a forced transient failure clearing within
  `MaxRetries` is invisible to the caller; this last check is what caught
  the `pkg/retry.Wrap` bug in the aha above).

### Decisions

- **One shared `callSafely`, not a separate wrapper per failure mode.**
  Panic recovery and the hard timeout both needed a goroutine boundary
  around `consumerFunc` for different reasons (catch a same-stack panic;
  give the caller an exit door independent of the callee) — building them
  as one helper instead of two meant the panic-recovery `defer` only had
  to be threaded through the timeout race once, not duplicated.
- **The panic-recovery `defer` moved from `callSafely`'s own frame into the
  spawned goroutine, mid-review, once the timeout race landed.**
  `recover()` only catches a panic on the same goroutine's call stack. An
  earlier draft assigned a recovered panic straight into `callSafely`'s
  named return from the child goroutine — both a data race (unsynchronized
  write racing the parent's own `return`) and functionally silent
  (`done` never received anything on a panic, so `callSafely` waited out
  the *entire* timeout+grace before returning the wrong, generic timeout
  error instead of the real one). Fixed by having the goroutine's own
  `recover()` send into `done` like any other result — verified with
  `-race`: a throwaway test panicking `consumerFunc` returned in ~120µs
  with the panic message and stack intact, no race.
- **`AbandonedRoutines.Remove` runs after `recover()` in the same defer,
  not before.** If `Remove` panicked first (e.g. a nil `Metrics`), it would
  prevent `recover()` from ever running, undoing the panic-recovery
  guarantee entirely. Considered and declined: a nil-guard on `Metrics`
  inside `callSafely` itself, or a second nested `defer`/`recover` around
  just the `Remove` call — declined because `validate()` already guarantees
  `Metrics != nil` before a `WorkConsumer` can be used, and this codebase's
  convention is to trust that guarantee once, at construction, rather than
  re-checking it at every call site downstream.
- **"Track abandoned goroutines" stayed in Phase 9; lease heartbeat/renewal
  moved to a new deferred `9b`.** The registry (the `(message,
  attempt)`-keyed map + raw counters) is this phase's own lab dependency —
  the lab already asserts against a live gauge — so it stayed here as
  plain in-process state, with the metrics-library wiring deliberately
  deferred to Phase 10 instead of inventing a one-off shape early. Lease
  heartbeat/renewal has no such dependency on anything already shipped and
  is its own substantial mechanism (Temporal-style activity heartbeats),
  so it was cut to its own section rather than padding this phase's scope
  for a "while we're in here" reason.
- **The `pkg/retry.Wrap` bug fix is a plain early-return, not a rewrite of
  the classification logic.** `if err == nil { return nil }` ahead of the
  `IsRetryable` check — success was always meant to short-circuit before
  classification even runs, since "is this permanent" is a meaningless
  question for a nil result. Verified across all four shapes (retry-then-
  succeed, immediate success, exhausted retries, immediate permanent
  failure) and against the full existing lab suite with no other changes
  needed anywhere — the bug was purely local to `Wrap`'s own early-return
  branch, not a design flaw in the retryable/permanent split itself.

---

## Phase 10 — Observability: logging & the rollup model

**What it does, and the tradeoff:** before this phase the only way to see
consumer health was to query Postgres directly, and Phase 9's abandoned-
goroutine counters lived as informal in-process state with nowhere to go.
This phase adds the operator-facing surface — a pluggable logger, a single
queue-state query merging DB-truth (backlog, inflight, exception counts,
oldest-unacked age, open leases) with the in-process counters into one
metrics snapshot, an OTel `metric.Meter` integration built on that same
snapshot, and a debug readout that's the free human-readable consumer of
the identical data the OTel instruments expose to machines — then spends
its own dedicated bullet settling the lazy-vs-synchronous `AdvanceWaterline`
question deliberately deferred back in 6.5b. The tradeoff decided there:
lazy costs staleness (a few seconds under the real default `ClaimPollRate`)
in exchange for zero throughput cost, since `Commit` today never touches
`cursors` at all; synchronous buys near-zero staleness but adds an `UPDATE
cursors` that serializes every concurrent committer in a group on a row
`Commit` currently never contends on. Measured, not assumed: 1.3x-1.9x
slower at 20 concurrent committers is a permanent tax on every commit to
buy a sub-5-second wait down to a few milliseconds — a bad trade for what
retention drop decisions and a debug readout actually need. Stayed lazy;
added `WaterlinePollRate` so staleness is independently tunable without
buying the contention.

**The aha:** the OTel export lab's first run failed, and the failure was
itself proof the lab was doing real work, not a rubber-stamp check. All 10
`QueueState` gauges showed up on the scraped Prometheus body immediately,
but all 3 `AbandonedRoutines` instruments were silently absent —
`ObservableGauge`s report every collection cycle via their callback
regardless of whether anything happened, but `Counter`/`UpDownCounter`/
`Histogram` only emit a data point once something actually calls `.Add()`/
`.Record()`. The lab's first driving-activity step only ever induced a
retryable failure, never an abandon, so those three synchronous instruments
correctly had zero data points — not a bug in `pkg/consumer/metrics`, a gap
in the lab's own coverage. Fixed by adding a real hard-timeout hang past
`WorkTimeout+Grace` and waiting for self-clear before scraping; re-run
confirmed all 13 instruments on the wire.

### Explain it back

**1. What's the tradeoff between a lazy periodic rollup and a synchronous
one — what do you gain and what do you pay for each?**

Answer: for lazy - its an async rollup so you have some lag between what
has actually been processed vs where committed sits. This lag causes
partition drop and deliveries sweep to have a few seconds of lag. However
b/c it is lazy the committed movement is off the hot path and so that
cursor movement does not slow or degrage throughput.
for synchronous - it is mostly the opposite. Partition drops and delivery
sweeps happen nearly right after committed changes (no lag) which better
shows exactly where committed is. but it is at the cost of an extra query
on the claim release hot path which slows down throughput. Specifically
this isn't just an extra query's fixed cost -- `Commit` today never touches
`cursors` at all (only `leases`/`deliveries`), so concurrent committers in
the same group commit fully in parallel right now. A synchronous rollup
adds an `UPDATE cursors` that those same committers now serialize on, which
is why the 20-worker case measured 1.3x-1.9x slower, not just the flat
+30-50% fixed-cost hit.

**2. Why does a live debug readout of claimed/committed/exception-count
matter even though the underlying data was always queryable in Postgres
directly?**

Answer: its a better developer experience, they don't have to know the
underlying typology they just call a method

**3. For each number in the metrics snapshot: which failure mode is it the
early warning for?**

Answer:
backlog - the classic consumer lag metrics. Means you are trailing behind
head which is normally not good.
exceptions dead - how many messages have truly failed, how numbers normally
indicate a bug or outage
abandoned total / self-clear - number of routine timeouts and how long they
take to resolve if they do. Can indicate not handled ctx close or async
code hanging
inflight (claimed-committed gap) - batches out for processing right now;
distinguishes rollup lag from real backlog
ready exceptions - retry queue depth building up
inflight exceptions - currently mid-retry
oldest unacked age - flags a single stuck message even when the counts
otherwise look fine
open leases - a crashed/never-committed consumer, exactly what
scenarioCrash in metricsreactionlab exercises
abandoned outstanding - goroutines hung right now, vs. total's lifetime
count

**4. Why does the OTel integration depend on
`go.opentelemetry.io/otel/metric` (the API package) but never the SDK or a
specific exporter like Prometheus's or Datadog's client?**

Answer: go.opentelemetry.io/otel/metric is only api code ie very light not
many dependencies go.opentelemetry.io/otel has a lot of extra code and
dependencies that make this library heavier

### Done

- **Common logger interface + writer-based default.** `pkg` logging accepts
  an interface, not a hardcoded implementation; a default `io.Writer`-backed
  implementation ships for callers with no opinion.
- **Queue-state query.** One datastore method computing, live, per
  `(group, topic)`: backlog (`head - committed`), the `claimed - committed`
  inflight gap, `ready`/`inflight`/`dead` exception counts, oldest-unacked
  age, and open-lease count — DB-truth, generalizing the ad hoc `just lag`
  recipe. Everything else in the phase reads through this one query.
- **Metrics snapshot.** Merges the query above with Phase 9's
  `AbandonedRoutines` in-process counters into one struct/method — the
  single source both the debug readout and the OTel instruments read from.
- **OpenTelemetry metrics integration.** Accepts a `metric.Meter`
  (API package only, defaulting to the global no-op provider so an
  unconfigured caller pays zero cost). `ObservableGauge` per DB-truth
  number (callback re-runs the query), `Counter`/`UpDownCounter` for the
  in-process ones. Precedent: River's `rivercontrib/otelriver` — API-only
  in the core module, vendor wiring lives outside it.
- **Debug/metrics readout.** A `String()`/print method formatting the
  snapshot for a human, scoped per `(group, topic)`.
- **Resolved the lazy-vs-synchronous rollup — stayed lazy.** See tradeoff
  above; added `WorkConsumerConfig.WaterlinePollRate` (0 defaults to
  `ClaimPollRate`, same pattern as `JanitorPollRate`).
- **Three labs.** `metrics-reaction-lab` (drives all four harness failure
  shapes through the real `WorkConsumer`/`Datastore` paths, diffs the
  snapshot before/after, asserts each failure moves EXACTLY the number(s)
  it should and nothing else — all six tracked numbers moved at least once,
  each attributable to a distinct trigger); `metrics-load-lab` (runs the
  real `Consume` loop under a pre-seeded burst at a slow vs. fast
  `WaterlinePollRate` — measured a 15.6x cut in catch-up time (2.03s to
  130ms) in one representative run, consistent with rollup-lab's own
  staleness-scales-with-poll-interval finding now proven through the real
  ticker-driven goroutines; a second scenario injects retryable failures
  live and watches ready exceptions climb and fully drain to zero with no
  manual intervention); `otel-export-lab` (the only place in the repo that
  imports `otel/sdk` or a specific exporter — wires a real
  `sdkmetric.MeterProvider` backed by `otel/exporters/prometheus`'s Reader,
  drives real activity including a hard-timeout abandon+self-clear, scrapes
  over a real `httptest` HTTP server via `promhttp.HandlerFor`, confirms
  all 13 instruments present on the wire).

### Decisions

- **Stayed lazy, not synchronous.** See the tradeoff paragraph above and
  Q1 — the measured 1.3x-1.9x contention cost under concurrency was a
  permanent tax on every commit for every consumer group, to buy staleness
  that retention and a debug readout don't need at sub-5-second precision.
  `WaterlinePollRate` gives an operator a knob for a shorter wait without
  paying for it on the hot path.
- **OTel API-only, never the SDK or a specific exporter, in `pkg/`.** Kept
  the dependency boundary at `go.opentelemetry.io/otel/metric` so `pkg/`
  stays vendor-neutral and zero-cost by default (global no-op provider) —
  a caller wires up Prometheus, Datadog, or nothing, and `pkg/` never knows
  or cares. `otelexportlab` is the one place in the whole repo that imports
  the SDK or a concrete exporter, by design.
- **Metrics snapshot is the single source for both the debug readout and
  the OTel instruments — neither computes its own numbers.** Avoids two
  independent implementations of "what's the current queue state" ever
  drifting apart from each other.

