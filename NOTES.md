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

