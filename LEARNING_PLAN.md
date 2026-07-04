# Learning Plan: Building a Durable Message Platform on Postgres

A phased build plan designed as a learning path. Each milestone adds **one**
concept, builds on the last, and ends with a runnable lab that proves you
understand the mechanic — plus the real-system parallel so you're learning the
canonical pattern, not a toy.

The order matters: build the simplest durable atom first, then layer the log,
fan-out, lifecycle, routing, and ordering on top, refactoring as you go. The
refactors *are* the lesson — they're where you feel why each system made its
tradeoff.

Target capabilities:
- Retention + replay
- Fan-out to independent consumers
- Complex routing
- Per-message lifecycle
- Optional FIFO partitions

---

## Reference implementation (check your work — don't read ahead)

A complete, tested reference of this plan's **end-state** lives in
[`reference/waterline/`](reference/waterline/): the hybrid managed-cursor
("waterline") design — an append-only `events` log + a sparse `deliveries`
exception window + per-(group, lane) cursors, with routing, FIFO partitions, and
log compaction implemented and benchmarked. It is a runnable, adversarially-
reviewed **answer key**, not a substitute for building it yourself: peeking before
you've done a phase's lab and Explain-it-back defeats the point. Use it to *check*
your design after each phase, and to see how the pieces compose at the end.

- Run it / read the design notes: [`reference/waterline/README.md`](reference/waterline/README.md)
- Throughput vs. the SQL targets (and the honest caveats): [`reference/waterline/BENCH_RESULTS.md`](reference/waterline/BENCH_RESULTS.md)
- The six correctness invariants it must hold: [`bench/scale/waterline_design_v2_hybrid.md`](bench/scale/waterline_design_v2_hybrid.md)

| Phase | Compare your work against (in `reference/waterline/`) |
|---|---|
| 1–2 lifecycle | `deliveries` + `ClaimExceptions`/`Ack`/`Nack`/`DeadLetter` (`pglog.go`) |
| 1.5 txn enqueue | `AppendTx` (`append.go`) |
| 3 / 3.5 commit wall | `AckBatch` (the batch-ack lever); `BENCH_RESULTS.md` measures the fsync wall |
| 4 log/queue split | `events` + `cursors`; replay = reset the cursor |
| 5 fan-out | independent `cursors` rows per group |
| 6 lifecycle-on-log (naive per-row) | the `deliveries`-row-per-(group,event) model — what you measure the write-amplification wall on |
| 6.5 claim-from-log refactor | `pglog.go` `Claim`/`Commit` (range-claim, no per-event rows) + sparse `deliveries` exception window + `leases` + `Advance` waterline + `sharding.go` lanes |
| 7 routing | `routing.go` — NATS topic regex (this phase simplifies to a true `*` wildcard instead; header JSONB `@>` deferred to optional Phase 7b) |
| 8 retention/compaction | `compaction.go` — keep-latest-per-key, tombstones, watermark-safe |
| 12 FIFO partitions | `partitions.go` — at-most-one-in-flight-per-key, FIFO-through-retry |

One honest delta between this plan and the reference, worth knowing before you
compare: a group is **either** a cursor/happy-path consumer **or** a
FIFO/`deliveries` consumer, not both at once (the reference enforces this; the plan
builds them as separate modes in Phases 6.5 and 12). This plan's own
`pkg/consumer.Datastore` interface goes further than the reference and supports
*both* modes behind one interface (`CURSOR` and `LIFECYCLE`), which is exactly
why it has more methods than the reference's `Log` interface — worth knowing
before Phase 11 ("bloat" there isn't automatically waste).

---

## Status board

Update this as you go. One line per phase; the current phase gets the detail.

| Phase | State | Notes |
|---|---|---|
| 0 — Setup | ✅ done | golang-migrate + justfile + docker-compose wired |
| 1 — Durable atom | ✅ done | tagged `phase-1` |
| 1.5 — Transactional enqueue | ✅ done | answers in NOTES.md; tag `phase-1.5` |
| 2 — Lifecycle | ✅ done | answers in NOTES.md; tag `phase-2` |
| 3 — Competing consumers | ✅ done | answers in NOTES.md; tag `phase-3` |
| 3.5 — Throughput / the commit wall | ✅ done | answers in NOTES.md; tag `phase-3.5` |
| 4 — Log/queue split | ✅ done | `message_log` + per-group cursor; **`ORDER BY id`** is load-bearing (high-water mark must be ordered); answers in NOTES.md; tag `phase-4` |
| 5 — Fan-out | ✅ done | `-group` flag → independent `cursors` row per group over one log; poll loop + `just lag` (head − position); answers in NOTES.md; tag `phase-5` |
| 6 — Synthesis (naive per-row) | 🔨 **next** | `deliveries` row per (group,event); **measure the write-amplification wall** |
| 6.5 — Claim-from-log refactor | ✅ 6.5a–c done | 6.5a happy path (`phase-6.5a`), 6.5b leases + crash recovery (`phase-6.5b`), 6.5c exception window + poison-batch quarantine (`phase-6.5c`) — all tagged, answers in NOTES.md; **6.5d (lane sharding) deprioritized — moved to the end of this document, optional, not started** |
| 7 — Routing | ✅ done | predicate at read/fan-out time (cursor or per-row); a true `*` wildcard, not NATS-style depth-precise selectors — header matching cut to **7b**; answers in NOTES.md; tag `phase-7` |
| 7b — Header/content routing | ⬜ | deprioritized — optional, deferred; moved to the end of this document |
| 8 — Operational layer | ⬜ | retention, compaction, observability — **current focus after 7** |
| 9 — Consumer fault isolation & recovery | ⬜ | panics, hard timeouts, DB-blip retry, graceful shutdown (from TODO.md) |
| 10 — Observability: logging & rollup model | ⬜ | pluggable logger, debug readout, lazy-vs-sync waterline (from TODO.md) |
| 11 — Architecture cleanup | ⬜ | datastore boundary, producer fan-out, attempt audit log (from TODO.md) |
| 12 — FIFO partitions | ⬜ | deprioritized — ordering opt-in on the lifecycle path, not current focus |

**Naming drift to resolve:** the plan's Phase 1 table is `jobs`; the migration
created `message_log`. That name leans "log" while Phases 1–3 are pure *queue*
semantics — Phase 4 is where a log table earns the name. Either rename now to
`jobs` and let Phase 4's `events` table be the big rename moment (recommended —
the rename *is* part of feeling the split), or keep `message_log` and note the
dissonance in NOTES.md.

---

## How to work each phase

Every phase has five sections. Work them in order:

1. **Concept** — read before coding.
2. **Build** — checkboxes. Small, ordered.
3. **Lab** — runnable experiments that prove the mechanic. Don't trust the
   code; trust what you watched happen. Each lab should be reproducible from a
   `just` recipe so you can re-run it months later.
4. **Explain it back** — active-recall questions. Answer them **out loud or in
   NOTES.md from memory, without looking at the code**. If you can't, the
   phase isn't done. This is the part that makes it stick.
5. **Done when** — exit criteria. When met, write the NOTES.md entry and
   `git tag phase-N`. Tags give you a diffable history of the refactors —
   `git diff phase-3..phase-4` will *show* you the log/queue split.

**NOTES.md format** (the real deliverable): one entry per phase —
*"System X does this by ___, and the tradeoff is ___"* plus your answers to
the Explain-it-back questions.

---

## The lab harness (build alongside Phase 1)

Nearly every checkpoint in this plan says "kill a worker", "crash
mid-process", "slow a consumer", or "fail a message". You can't do those by
hand reliably — build the knobs once, early, as flags on the example
producer/consumer binaries:

- [x] `producer --count N` — enqueue N messages in a loop (with distinct
      payloads so you can trace individual messages).
- [x] `consumer --sleep 2s` — artificial processing time, so there's a window
      to kill it mid-process.
- [x] `consumer --fail-rate 0.2` — consumerFunc returns an error randomly.
- [x] `consumer --crash-after 3` — `os.Exit(1)` mid-transaction after the Nth
      message. Simulates a real crash (no rollback call, no cleanup).
- [x] `just` recipes: `just produce N`, `just consume`, plus a way to run two
      consumers side by side (two terminals is fine).
- [x] A peek query you'll run constantly:
      `just peek` → `SELECT id, payload, created_at FROM jobs ORDER BY id;`
      (extend it with status/attempts columns as the schema grows).

These knobs are not throwaway — they become your failure-injection suite for
every later phase, and in Phase 8 they're how you exercise the metrics.

---

## Phase 0 — Setup & the destination ✅

**Goal:** know where you're headed so each phase has a place to fit.

- [x] Stack: Go + Postgres (docker-compose locally, Railway Postgres available).
- [x] Migration tool: `golang-migrate`, driven by `justfile` recipes — you'll
      evolve the schema ~8 times, so migrations are part of the discipline.
- [x] Internalize the end-state you'll arrive at in two moves. The destination is
      a **claim-from-log** platform, not a row-per-message one:
  - `events` — immutable, append-only **log** (retention, replay, routing,
    partitions live here). Never deleted on consume.
  - the **happy path is a CURSOR that claims ranges straight from the log** — in
    the steady state there is *no* per-message row at all. A group's progress is
    two integers per lane: `claimed` (read frontier) and `committed` (the
    waterline — every offset at/below it is resolved).
  - `deliveries` — a **sparse exception window**: a row exists *only* for a
    message that fell off the happy path (retry / dead-letter / out-of-order).
    Per-message lifecycle (ack/retry/DLQ) lives here, for the small exceptional
    fraction — not for every message.
  - `leases` — a crash-safe reservation of an in-flight claimed range, so a
    crashed worker's range is reclaimable.
- [x] You reach this in two moves, and **the refactor between them is the lesson**:
      first the *naive* split where EVERY (group, message) gets a `deliveries` row
      (Phase 6) — then you measure its write-amplification wall and **refactor to
      claim-from-log** (Phase 6.5), where the cursor carries the happy path and
      `deliveries` shrinks to just the exceptions.
- [x] Don't build any of it yet. Start with one table and split it at Phase 4 —
      feeling *why* each split is necessary is the point.

**Mental model to hold:** a **log** is "messages are facts you retain and re-read
at a cursor"; a **queue** is "messages are work you claim and consume." Every
system below is one, the other, or a deliberate fusion. You're building the
fusion.

---

## Phase 1 — The durable atom: append + atomic claim ✅

**Concept:** durability + exactly-one-worker-gets-each-message, the irreducible
core of any queue.

**Build:**
- [x] One table: `jobs(id bigserial pk, payload jsonb, created_at timestamptz default now())`.
- [x] A producer: `INSERT` a row.
- [x] A consumer that claims a row atomically with
      `SELECT * FROM jobs ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED;`
- [x] **`DELETE` the claimed row after the consumerFunc succeeds, inside the
      same transaction.** Claim → process → delete → commit is one atomic unit;
      that atomicity is the entire durability story of this phase.
- [x] **Poll loop:** the consumer currently runs one batch and exits. Make it
      loop: claim → process → sleep(poll interval) when the queue is empty.
      A worker that exits can't be "killed mid-process" — the labs need a
      long-running worker.
- [x] **Graceful shutdown** (your TODO.md item — it belongs here): trap
      SIGINT/SIGTERM, stop claiming new work, let the in-flight transaction
      finish (commit or roll back), then exit. The interesting question: what's
      the difference between a graceful stop and a crash, *from the queue's
      point of view*? (Answer: nothing the queue can't already handle — the tx
      either committed or it didn't. Graceful shutdown is about not *wasting*
      work, not about correctness. Prove that to yourself in the lab.)

**⚠️ Trap T1 — batch poisoning (you're already standing in it):** the current
consumer claims `LIMIT 5` and processes the batch in one transaction, returning
on the first error — so one bad message rolls back the whole batch, including
messages that processed fine (and any side effects they had already fired are
now duplicated on retry). Batching is a Phase 3 concept; for Phase 1, set the
batch limit to 1 so the claim unit and the commit unit are the same single
message. When you reintroduce batching in Phase 3, you'll do it knowing the
failure-domain question it raises.

**Lab:**
- [x] **Two workers, no collisions:** `just produce 20`, run two consumers with
      `--sleep 1s`, watch the printed messages interleave with no duplicates.
- [x] **The SKIP LOCKED contrast:** remove `SKIP LOCKED`, rerun. Watch worker 2
      block behind worker 1 and the workers serialize. Put it back. That
      contrast is the whole lesson of this phase.
- [x] **Kill mid-process:** run a consumer with `--sleep 5s`, `kill -9` it
      during the sleep, run `just peek` — the row is still there (tx rolled
      back, lock released). Start another consumer — it picks the row up.
- [x] **Crash-after:** same proof via `--crash-after`, no manual kill needed.

**The aha:** `SKIP LOCKED` is what lets two workers run the exact same query at
the same instant and get *different* rows instead of one blocking the other.
And a crashed worker needs zero recovery code — the transaction rollback *is*
the recovery.

**Explain it back** (from memory, no peeking):
1. Why does the `DELETE` have to be in the same transaction as the claim? Walk
   through what can go wrong with each of the two orderings if it's separate.
Answer: If delete is not in tx then the delete command could have a network blip error and we end up with completed work that is 'retried' essentially.
If delete is handled before processing and the worker crashes mid process the work is lost forever and never handled (worst case)
2. A worker is killed with `kill -9` mid-process. Step by step, what does
   Postgres do, and when does the row become claimable again?
Answer: When the connection is closed without a committed transaction it is assumed failed and is rolled back
3. What does `SKIP LOCKED` change about the query's *result set*, exactly? Why
   is that safe here when skipping rows would normally be a correctness bug?
Answer: a locked row can be assumed to be a row 'in process'. Because we don't want to double process work this is correct functionality

**Done when:** all labs pass, questions answered in NOTES.md, `git tag phase-1`.

**Real systems:** RabbitMQ `basic.consume` + `ack` (ack deletes the message);
AWS SQS `ReceiveMessage` + `DeleteMessage`. `SKIP LOCKED` is Postgres's
competing-consumers primitive.

---

## Phase 1.5 — Transactional enqueue (the killer feature)

**Concept:** because the queue is a *table in the same database* as your business
data, you can `INSERT` the job in the **same transaction** as the business write.
Both commit or neither does. This is the single biggest reason to build a queue
on Postgres — and the one thing Kafka and RabbitMQ structurally cannot offer.

**The dual-write problem (why this matters):** with an external broker you do two
writes to two systems with no shared transaction — write the business row to the
DB, then publish to the broker. There is no safe ordering:
- DB commits, publish fails → you did the work but lost the event.
- Publish succeeds, DB rolls back → phantom event for work that never happened.

No amount of retry logic fully closes this gap. It's the fundamental integration
pain of external brokers.

**Build:**
- [x] Add a toy business table (e.g. `users`) via a new migration.
- [x] An API shape question worth sweating: the producer currently owns its own
      pool and transaction. To enqueue inside the *caller's* transaction, the
      producer needs to accept a `pgx.Tx`. Design that (e.g.
      `Produce(ctx, tx, work)` or a `ProducerInTx` variant) — this exact API
      tension is why River's docs talk about "insert-only clients."
- [x] Wrap a business write + a `jobs` INSERT in a single `BEGIN ... COMMIT`.

**Lab:**
- [x] Force an error (rollback) *after* the business write but *before* commit;
      `just peek` both tables — confirm **neither** row exists.
- [x] Commit successfully; confirm **both** exist and a worker picks up the job.
- [x] **Visibility proof:** inside an open (uncommitted) producing transaction,
      run a consumer — confirm it cannot claim the job until commit. Atomicity
      on the producer side and isolation on the consumer side compose for free.

**The aha:** "do the thing and durably record that follow-up work is needed" in
one atomic step. Kafka/RabbitMQ can't do this because they're separate systems
from your database — the transaction boundary doesn't reach them.

**Forward reference — the Outbox pattern:** if you later need the event to reach
an *external* system (another service, or Kafka itself), this same table becomes
a **transactional outbox**: business write + outbox insert are atomic, and a
separate **relay** process reads the table and forwards downstream. That's how
you bridge to external systems *without* the dual-write problem. You don't need
to build the relay now — just know the table you already have is the outbox.

**Explain it back:**
1. Describe the dual-write problem to an imaginary colleague, including why
   neither write ordering is safe and why retries don't fix it.
2. Why can't a consumer ever observe a job from an uncommitted producer
   transaction? Which ACID property is doing the work?
3. What is the outbox pattern, and what part of it have you already built?

→ Answered (corrected) in NOTES.md, "Phase 1.5 — Transactional enqueue".

**Done when:** labs pass, NOTES.md entry written, `git tag phase-1.5`.

**Real systems:** the **Transactional Outbox** pattern (microservices.io);
Debezium / Change-Data-Capture as the relay that reads the WAL and forwards.
This is precisely what Kafka and RabbitMQ *cannot* do natively — and exactly what
DB-backed job libraries (River, Oban, graphile-worker) advertise as
"enqueue inside your transaction."

---

## Phase 2 — Per-message lifecycle (the part you care about most)

**Concept:** a message isn't just present/absent — it has a *state machine*.
This is the feature Kafka can't do natively and RabbitMQ hides from you.

**Build:**
- [x] Migration: add `status text` (`ready|processing|done|dead`),
      `attempts int default 0`, `run_at timestamptz default now()`,
      `locked_at timestamptz`, `last_error text`.
- [x] Claim changes to `WHERE status='ready' AND run_at <= now()`, and instead
      of deleting: `UPDATE status='processing', locked_at=now(), attempts=attempts+1`.
      **Note the structural change:** claim and process are now *separate
      transactions*. The claim tx commits fast; processing happens unlocked.
- [x] On success → `status='done'` (keep the row for now, so you can see history).
- [x] On failure → if `attempts < max`: `status='ready', run_at = now() + backoff(attempts)`
      (exponential backoff); else `status='dead'`.
- [x] **Lease / visibility timeout (reclamation):** recover messages from workers
      that crashed mid-process. An expired lease is just another claimable row, so
      **fold reclamation into the claim** rather than running a separate reaper
      daemon — widen the claim's eligibility predicate to
      `WHERE (status='ready' AND run_at <= now())
             OR (status='processing' AND locked_at < now() - interval 'X')`.
      Every normal claim then reclaims an expired lease as a side effect: no
      coordinator, no singleton, workers stay symmetric (the pull model's whole
      point). The lock is still only held for the fast claim tx — the long-lived
      "I'm working on this" is the *data* lease (`status`+`locked_at`), not a DB
      lock. Reclaimed rows flow through the same `attempts=attempts+1`, so a
      repeatedly-crashing worker eventually hits max-attempts → `dead`.
      *Naive alternative:* a standalone periodic `UPDATE ... SET status='ready'
      WHERE status='processing' AND locked_at < ...`. It's idempotent and safe to
      run from any/all workers, but a dedicated reaper *process* drifts toward the
      centralized server we're avoiding. Decision: fold-into-claim.

**⚠️ Trap T2 — the zombie worker:** after the lease expires and the reaper
re-readies a message, the *original* worker may still be alive and slow — now
two workers are processing the same message. Your consumerFunc just became
at-least-once. You don't have to solve it in this phase (idempotency is the
real answer), but induce it in the lab and name it in NOTES.md.

**Lab:**
- [x] `--fail-rate 0.3` with `max=3`: watch attempts climb, `run_at` push into
      the future (backoff), and stubborn messages land in `dead`.
- [x] `SELECT * FROM jobs WHERE status='dead'` — that's your dead-letter queue,
      as a query. You can *see* every message's state, attempt count, and error.
- [x] `--crash-after` a worker mid-process; watch the reaper return its job
      after the lease expires and another worker complete it.
- [x] **Induce T2:** set `--sleep` longer than the lease. Watch the same
      message processed twice. Feel it.

**The aha:** the `FOR UPDATE` lock only lasts the *claiming* transaction — once
you commit the `processing` update, the lock is gone. So the durable "I'm
working on this" lease is the `status`+`locked_at` *data*, not the DB lock.
Getting this distinction is the single most important insight in the whole plan.

**Explain it back:**
1. In Phase 1, what held the claim? In Phase 2, what holds it? Why did it have
   to change? (Hint: what would a 10-minute job do to a Phase 1 transaction?)
Answer: Phase1 the db lock, Phase2 the db row data (status='processing' and locked_at).
A long running job in phase1 would hold open a transaction the entire lifecycle. With high concurrency a huge number of connections would remain open which is not scalable. With phase2 we have a millisecond lock and instead rely on queries and row data to understand what is 'locked' vs not.
2. Walk the full state machine including every transition's trigger.
Answer: 
- 1. Select and lock work from queue
- 2. Update work to 'processing' -> release lock
- 3. Do consumer job on work
- 4a. If success record success on work row in db
- 4b. If failure:
  - i. If has attempts left -> retry work
  - ii. If no attepts left -> mark as dead (do not retry)
3. Why does lease reclamation make delivery at-least-once rather than
   exactly-once? What property must the consumerFunc now have?
Answer: If the consumerFunc takes longer than the lease than another concurrent worker could pickup the work as it is considered reclaimable
To fix that the lease timeout must be greater than the consumerFunc timeout OR the consumerFunc must be idepotent

**Done when:** labs pass (including T2 induced and understood), NOTES.md,
`git tag phase-2`.

**Real systems:** RabbitMQ `nack`/`reject` + Dead-Letter Exchanges; SQS
visibility timeout + redrive-to-DLQ; Pulsar negative-ack + `maxRedeliverCount` →
DLQ; JetStream `maxDeliver` + `term`. The `run_at`+backoff is SQS/JetStream
delayed redelivery.

---

## Phase 3 — Competing consumers & batching 🔨

**Concept:** scale throughput by adding workers without double-processing, and
amortize round-trips. The shape: **one dispatcher batch-claims into a bounded,
lease-aware buffer; a worker pool drains it.** This is the prefetch + worker-pool
pattern — exactly how a RabbitMQ/SQS consumer with a prefetch count actually
works. Reuse `pkg/concurrency`: `PressureQueue` is the buffer, `WorkerPoolLimiter`
is the concurrency cap. (The `examples/simple/server.go` scrape loop is this
pattern for *ephemeral* work — Phase 3 is porting it to *durable, leased* work,
which is where it gets interesting.)

**Why this shape (vs N symmetric workers):** N independent workers each looping
claim→process→ack is the simpler option and is *throughput-equivalent when work
is slow* (both converge to N/W — see the derivation in NOTES Phase 3). The
dispatcher+pool wins on the two things that *don't* converge: it absorbs
**variance in work time** (a slow message ties up one worker, not a whole
worker's serially-processed batch), and it collapses **connection count** (1
claimer + 1 ack-batcher + N pure-compute workers = 2 DB connections regardless of
N, instead of ~N). If those don't matter for a given workload, the N-worker loop
is the honest simpler choice — record why you picked what you picked.

**Build:**
- [x] **Dispatcher loop:** batch-claim `min(batchLimit, freeBufferSlots)` rows and
      push each into the `PressureQueue`. Gate the claim on `CanEnQueue` — you must
      **never** hit the `EnQueue`→`ErrQueueFull` drop path, because you cannot drop
      a row you've already leased. Backpressure flows *backwards*: a full buffer
      means stop claiming.
- [x] **Worker pool:** N goroutines drain the buffer (acquire permit → dequeue →
      process → ack → release permit). Reuse `WorkerPoolLimiter`.
- [x] **Bound the buffer to the lease — the real lesson of this phase.** Every
      item in the buffer is a *ticking lease*: `locked_at` was stamped and
      `attempts` already incremented at claim time. If an item waits in the buffer
      longer than `workTimeout + buffer`, another process reclaims it and you
      double-process work *that was never even touched* — and idle items burn
      attempts toward `dead`. So prefetch **shallow**: buffer depth ≈ one batch,
      sized so N workers drain it well inside the lease window. The buffer is a
      worker-feeding tray, not a reservoir. (This is the hazard that did *not*
      exist for the ephemeral scrape queue, where losing/redoing work was free.)
- [x] Reintroduce batch-claim — `LIMIT 50 FOR UPDATE SKIP LOCKED` — and answer the
      Trap T1 question deliberately: with Phase 2's state machine, claim is its own
      fast transaction and each message's success/failure is recorded
      *individually*. One bad message no longer poisons the batch. Write down in
      NOTES.md why the Phase 1 batch failure-domain problem dissolved.
- [x] Add the **critical index**: a partial index
      `CREATE INDEX ON message_log (run_at) WHERE status='ready'`. Without
      it the claim table-scans as terminal rows accumulate.
      **Caveat from the Phase 2 fold-in:** the claim also matches
      `status='processing' AND locked_at < ...`, so a `WHERE status='ready'`-only
      index doesn't cover the expired-lease branch. While the `processing` set
      stays small that branch is cheap; if it grows, add a second partial index on
      `locked_at WHERE status='processing'`. (Phase 3.5's archive lever shrinks
      this problem from the other direction.)
- [x] **ctx-aware shutdown.** `DeQueue` blocks on `<-channel` with no ctx, so a
      parked worker won't notice cancellation. On shutdown: stop the dispatcher
      first, then close/drain the buffer so workers fall through — any
      un-dequeued leased rows just get reclaimed later (durability holds). Note
      this gap; it's the tax for reusing an ephemeral-work primitive for durable
      work.

**Lab:**
- [x] **Measure the index:** seed a few hundred thousand rows (mostly
      `done`/`dead`), `EXPLAIN ANALYZE` the claim query with and without the
      partial index. Record both numbers in NOTES.md. The index is the
      difference between a queue that stays fast and one that rots.
- [x] **Find the ceiling:** plot throughput vs worker count (rough numbers are
      fine — msgs/sec at 1, 2, 4, 8, 16 workers). Find where it stops scaling.
      Knowing where Postgres-as-a-queue tops out (tens of thousands/sec) tells
      you when you'd ever need Kafka. Record the ceiling and your guess at the
      bottleneck. (Phase 3.5 is where you push this number.)
- [x] **Variance proof:** mix a few deliberately slow messages (`--sleep`) into a
      fast stream. Confirm the pool keeps draining the fast ones while the slow
      ones occupy single workers — the thing an N-serial-batch worker structurally
      can't do. This is the dispatcher+pool's reason to exist.

**Explain it back:**
1. Why is the partial index so much better than a full index on `(status, run_at)`
   for this workload?
Answer: I only seeded around ~1000 and it is already apparent. The main difference is between a bitmap heap scan (with partial indexes) vs a full sequential scan (without indexes). Full scans are just much slower than a map lookup. In the actual time case with ~1000 rows it was .215 (without index) and .05 (with index) and this read difference I would assume only grows when not using index
2. Batch claiming in Phase 1 had a failure-domain problem. Why doesn't Phase 3's
   batching have it?
Answer: I'm not 100% sure but I do know with Phase1 we held the claim / lease via a lock at the db level. While this is effective it is not scalable. Each in process claim holds open a connection the entire time processing is occurring. For a long running job with many concurrent workers and many consumers this is a resource nightmare for the postgres database.
3. What was your measured ceiling, and what do you think the bottleneck was —
   lock contention, WAL, round-trips, or the worker code itself? How would you
   tell?
Answer: you did this analysis but with current topology acks are the bottleneck. With a single ack per commit the full roundtrip is costly. We could batch commits but with upcoming changes in topology soon it may not be worth it.
4. Why must the in-memory buffer stay shallow? Walk through what goes wrong with a
   deep prefetch buffer that did *not* go wrong for the scrape queue in
   `examples/simple`.
Answer: A deep prefetch buffer means that claimed work and their associated leases will regularly be lost while waiting in the buffer. While their is logic to handle this it is not perfect and can lead to excess double processing. Additionally it is just extra unnecessary work the pressure queue only needs to have a shallow buffer that hides or masks the claim sql latency such that it improves throughput.

**Done when:** all measurements recorded with numbers, NOTES.md,
`git tag phase-3`. **Phases 1–3 are a production-grade job queue** — this is
literally what River/Oban/graphile-worker are. Pause here and skim River's
docs; you'll recognize everything.

**Real systems:** Kafka consumer group (one partition → one consumer); RabbitMQ
prefetch count + a consumer worker pool; SQS batch receive. Batching = Kafka
`max.poll.records`.

---

## Phase 3.5 — Throughput: the commit wall (measure, don't over-build)

**Concept:** Phase 3 maxed out *concurrency*; this phase is about *commit rate*.
The cost model (NOTES Phase 3) says throughput converges to "how fast Postgres
commits the claim+ack writes" — **2 durable writes per message** — and the
fsync-per-commit is the wall. **But Phase 4 is about to change the data topology
out from under this**, so the discipline here is *measure the wall and apply only
the portable fix; don't optimize a physical layout you're about to refactor away.*

That deferral is itself the lesson: of the four obvious levers, three are coupled
to the queue topology and either **dissolve or relocate** when Phase 4 replaces
the mutable table with an append-only log + cursor. The cursor model *is* the
limit case of two of them. So we measure all four conceptually, build only the
one that survives, and forward-reference the rest to where they actually live.

**Build (the portable core):**
- [ ] **Measure the ceiling precisely.** Extend Phase 3's find-the-ceiling lab into
      a recorded baseline: msgs/sec at saturation, and *which* resource is the wall
      (WAL/fsync, claim-lock contention, round-trips, or worker code). This number
      is your "when would I ever need Kafka" threshold — it carries forward
      unchanged through every later phase.
- [ ] **`synchronous_commit` (the one lever that survives the topology change).**
      Relax it (`local`/`off`) and re-measure commit rate. The point you must be
      able to articulate: **this is safe *because* you already accepted
      at-least-once in Phase 2.** A lost ack on crash = a reclaim = a reprocess,
      which idempotency already covers — you're not buying new risk, just cashing
      in risk you already priced. (It would *not* be free for a bank ledger; that's
      the contrast.) It's a global Postgres knob, so it applies identically to the
      cursor model and the `deliveries` model later.

**Measure-only (understand the lever, then watch Phase 4 subsume it):**
- [ ] **Batch-ack / group-commit** — quantify it, don't build heavy infra. Acking B
      messages in one `UPDATE … WHERE id = ANY($1)` collapses B fsyncs into ~1.
      Note *why this is the bridge to Phase 4*: the cursor is the **ultimate**
      batched ack — N messages "acked" by a single integer write
      (`UPDATE consumers SET position = $last`). You're about to get this lever for
      free as a side effect of the topology, so feel its cost now and let Phase 4
      eliminate it.

**Deferred (topology-coupled — cross-referenced to their real home):**
- [ ] **Archive terminal rows → Phase 8 (retention).** An append-only log has no
      `done`/`dead` rows to archive; the "bloat" concern becomes "the log grows
      forever," solved by time-partition-drop retention in Phase 8. Building a
      `message_archive` table now would be thrown away at Phase 4. (It *does* return
      for the `deliveries` table in Phase 6 — note it there.)
- [ ] **Claim-hotspot sharding → Phase 12 (partitions) / Phase 6 (deliveries).** The
      single-hot-index-end contention *dissolves* in the cursor model (no competing
      claim — each cursor reads its own `offset > position` range). It returns only
      when competing claims return on `deliveries` (Phase 6), and sharding is
      exactly what Phase 12's `partition_key` formalizes.

**Lab:**
- [ ] Record the baseline ceiling, then re-measure after `synchronous_commit` and
      (on paper or a quick spike) after batch-ack. Note which moved it most.
- [ ] **Crash-after-async-commit:** with `synchronous_commit=off`, kill mid-batch
      and confirm reclamation reprocesses the lost acks — at-least-once still holds.
      This is the lab that *proves* relaxing the commit didn't add risk.

**Explain it back:**
1. Why is the fsync-per-commit the throughput wall, and why is the *ack* (not the
   claim) the half that's hardest to amortize in the queue model?
Answer: fsync-per-commit flushes data from mem to disk for every commit. The difference between operations in-mem vs on disk is costly in time. Because our current architecture makes an ack per work, unlike our claim process which does batching, a large amount of time is spent fsyncing / disk writing. Turning fsync off trades commit durability for speed however that is not an issue as we have a reclaimation process and a at-least-once fire policy. So the durability risk is not there. The intresting part is that at low throughput / concurrency fsync gives huge gains while at high throughput / concurrency it is more modest. That is because postgres automatically batches the fsync disk writes at high commit throughput so the fysnc off setting does less.
2. Why does the at-least-once contract make `synchronous_commit=off` a free lunch
   here when it wouldn't be for a bank ledger?
Answer: At least once means un ackd work CAN be processed again, double processing or technically even more. This does imply consumers must be idepotent but it allows us to lose unackd work because we will simply try it again via our reclaimation process. However for bank ledgers where exactly-once is required this is not a good fit. These normally need distrubuted transactions (or something like this)
3. Which of the four levers survive the Phase 4 topology change, and why do the
   other three dissolve or relocate? (This is the real point of the phase.)
Answer: synchronous_commit=off survives. Archiving rows is not needed for optimization as an indexed cursor will replace it. Batch acks are not needed because the cursor is the lifecycle tracker. Not sure what the third one is.

**Done when:** baseline + `synchronous_commit` ceilings recorded with numbers, the
batch-ack→cursor bridge and the two deferrals written in NOTES.md,
`git tag phase-3.5`.

**Real systems:** group commit (Postgres `commit_delay`, MySQL binlog group
commit); `synchronous_commit` trade-offs; the Kafka log model as the structural
end-state of ack-amortization (one offset commit per poll, not per message).

---

## Phase 4 — The log/queue split: retention + replay

**Concept:** stop deleting. Separate the immutable record of what happened from
the mutable record of who's processed it. This is the Kafka model, and the
foundation for everything after.

**Build (the big refactor):**
- [x] `events("offset" bigserial pk, topic text, payload jsonb, created_at)` —
      append-only, **never deleted on consume**. `offset` is the position.
      (Quoting note: `offset` is a reserved word in SQL — quote it or name the
      column `position`/`log_offset`.)
- [x] `consumers(name text pk, position bigint)` — one cursor per consumer.
- [x] A consumer reads
      `SELECT * FROM events WHERE "offset" > $position ORDER BY "offset" LIMIT N`,
      processes, then `UPDATE consumers SET position = $last`.
- [x] **Replay** = `UPDATE consumers SET position = 0` (or to a timestamp's
      offset). Re-reads history.

**Lab:**
- [x] Point a brand-new consumer at offset 0 and watch it replay the entire
      history independently of other consumers. That's the superpower Kafka has
      and RabbitMQ structurally cannot.
- [x] `git diff phase-3..HEAD` — read your own refactor. Which code got
      *simpler* (no status machine on the hot path) and what capability got
      *lost*? That diff is the queue↔log tradeoff, in your own code.

**The aha:** the cursor is a **high-water mark** — a single integer. This bare
cursor read *is* the claim-from-log happy path the whole platform is built on;
everything later either rides it (fan-out, routing) or adds a sparse side-table
for the cases it can't express. Note what you just *lost*: you can no longer say
"message 5 failed but 6,7,8 are done." That hole is the exact tension you'll
resolve in Phases 6–6.5. Feel the loss now.

**Explain it back:**
1. What exactly can a cursor not express that per-row status could? Give the
   concrete failure scenario.
Answer: per-row lifecycle. If a row fails you either have to stop / exit OR skip it.
2. Why does replay cost nothing extra in this design? What Phase 1 decision
   would have made it impossible?
Answer: because we have decoupled messages from reading position and messages are an append only log, you can freely process from any position in the log just by changing the cursor position. Phase 1 could never do this because we delete messages after processing.
3. When the consumer crashes *after* processing but *before* the cursor
   update, what happens on restart? What delivery guarantee does that imply?
Answer: In this case it would retry the already processed message. To that extent this is an at-least-once guarantee

**Done when:** labs pass, NOTES.md, `git tag phase-4`.

**Real systems:** Kafka (log + committed offsets in `__consumer_offsets`);
Pulsar (managed ledgers + per-subscription cursors). Retention-by-time is Kafka
`retention.ms`.

---

## Phase 5 — Fan-out to independent consumers

**Concept:** many consumers, each with their own cursor over the same log, each
at its own pace.

**Build:**
- [x] You already have `consumers.position` keyed by name — so multiple named
      consumers reading the same `events` is *already* fan-out. Formalize it: a
      `consumer_group` concept where each group has an independent position.
- [x] Compute **lag**: `(SELECT max("offset") FROM events) - position` per
      group. This is your health metric.

**Lab:**
- [x] Add a new group while the system runs; it starts at the earliest retained
      offset and catches up without affecting the others.
- [x] Slow consumer A to a crawl (`--sleep`); consumer B stays current. Watch
      their lags diverge. Independent consumption confirmed.

**The aha:** fan-out is free *because* you retained the log and made the cursor
per-consumer. Deleting on consume (Phase 1) made this impossible; retaining
(Phase 4) made it trivial. One design decision, two chapters apart, unlocks it.

**Explain it back:**
1. Why is fan-out structurally impossible in the Phase 1–3 design?
Answer: lifecycle is directly tied to the message log in phase 1-3 meaning it is a one-to-one mapping. Once it is processed by something anything else will also consider it processed. New design is one-to-many
2. What's the operational risk of a consumer group that's permanently slow,
   once retention (Phase 8) exists? (This is Kafka's "consumer fell off the
   retention window" failure.)
Answer: This is consumer lag at its extreme. This would mean it risks messages not being processed at all

**Done when:** labs pass, NOTES.md, `git tag phase-5`.

**Real systems:** Kafka consumer **groups**; Pulsar **subscriptions** (each
subscription is an independent cursor); JetStream **durable consumers**.

---

## Phase 6 — Lifecycle + fan-out: the per-row synthesis (and its wall)

**Concept:** give *each* consumer group per-message lifecycle (Phase 2) over the
*shared* log (Phase 4). The obvious way — the one every "lifecycle on a log"
system reaches for first — is a `deliveries` row per (group, event). Build that
here, get the full synthesis working, then **measure the wall it hits**: a row per
(group, event) is N× write amplification and brings back the claim hotspot. That
wall is exactly what Phase 6.5 refactors away with claim-from-log. This is the
plan's "the refactor *is* the lesson" turn — you have to feel the per-row cost to
understand why the cursor happy path matters.

**Build:**
- [x] New table:
      `deliveries(consumer_group text, event_offset bigint, status, attempts, locked_at, last_error, PRIMARY KEY(consumer_group, event_offset))`.
- [x] A "fan-out" step (a projector per group, or a lazy insert) materializes a
      `deliveries` row per (group, event) as events arrive.
- [x] Each group now claims *its own* delivery rows with `SKIP LOCKED` + the
      Phase 2 state machine. The `events` log stays immutable; lifecycle lives
      entirely in `deliveries`.
- [x] Keep a cheap path: broadcast/replay consumers that don't need lifecycle
      keep using a bare cursor (Phase 5). Only lifecycle-needing groups get
      delivery rows. Per stream, choose cursor vs delivery-rows.

*→ Don't peek at the reference yet: `reference/waterline/` implements the
claim-from-log endpoint you'll refactor toward in Phase 6.5, not the per-row model
you're building here. Build the per-row synthesis, measure its wall, THEN compare.*

**Lab — the synthesis demo (the one that proves you understand the whole
problem space):**
- [x] **Measure the wall (carry this number into Phase 6.5):** with G lifecycle
      groups, materialize + drain a few hundred thousand events and record
      msgs/sec and rows-written. Watch throughput scale *down* as you add groups
      (the N× write amplification) and as `done` rows pile up (the claim hotspot
      returns — Phase 3.5 lever 4). This baseline is what claim-from-log has to beat.

**The aha:** you can have retention/replay/fan-out **and** per-message
ack/retry/DLQ simultaneously — by separating the immutable log from mutable
per-consumer delivery state. Also feel the cost: delivery rows are N× writes for
N groups (write amplification). That cost is *why* Kafka punts lifecycle to
"retry topics" instead — and it's why Phase 6.5 stops writing a row per message
and lets the cursor carry the happy path, paying for a row only on the exceptions.

**Phase 3.5's levers come home here.** `deliveries` is a mutable, claimed,
status-bearing table again — so the queue-shaped optimizations you deferred apply
to it: competing `SKIP LOCKED` claims reintroduce the claim-hotspot contention
(lever 4 → shard by `consumer_group`/key), and accumulating `done` delivery rows
reintroduce the archive concern (lever 3 → archive or partition them, per Phase 8).
The append-only `events` log never needs either; only the lifecycle layer does.

**Explain it back:**
1. Draw this per-row architecture from memory: tables, who writes what, who reads
   what. (You've reached the *naive* synthesis — the claim-from-log end-state
   comes in Phase 6.5.)
2. Quantify the write amplification: 1000 events, 5 lifecycle groups, 2
   cursor groups — how many rows written?
3. For a given new stream, how do you decide cursor vs delivery-rows? Name the
   deciding question.

**Done when:** synthesis demo runs from one recipe AND the write-amplification
wall is measured with numbers, NOTES.md, `git tag phase-6`. You now have a working
log+queue platform — but a row-per-message one. **Phase 6.5 is the refactor that
turns it into claim-from-log; don't stop before it if throughput matters.**

**Real systems:** Pulsar **Shared** subscriptions with individual acks +
per-subscription DLQ (the canonical "lifecycle on a log"); JetStream
`AckExplicit`. Kafka's non-answer: separate retry/DLQ topics.

---

## Phase 6.5 — Claim-from-log: escape the per-row wall

**Concept:** Phase 6 wrote a `deliveries` row for *every* (group, message) and
measured the wall that creates. The fix is to stop doing that. The happy path goes
back to the **bare cursor of Phase 4–5** — it **claims a contiguous range straight
from the log** and processes it, writing **no per-message row at all**. You only
pay for a `deliveries` row when a message *fails* — a **sparse exception window**.
Two integers per lane carry the happy path (`claimed`, `committed`); the
exceptional fraction gets the Phase 2 retry/backoff/dead state machine (with
`processing` renamed `inflight` and the `done` state dropped — success is a
DELETE). This is the design the reference implements and the benchmark vindicated
(claim-from-log ran several× the per-row drain).

This is the densest refactor in the plan, so it's split into **four movements** —
each adds exactly one new survivable failure, ends in its own watchable lab, and
gets its own `git tag`:

- **6.5a — the happy path.** Claim a range, write no row, beat the baseline.
  (No crashes, no failures yet.)
- **6.5b — lease the range.** Survive a worker *crash* mid-range: reclaim and
  re-read the exact range.
- **6.5c — the exception window.** Survive a message *failure*: park only the
  failures, drain them pop-delete.
- **6.5d — shard the hot lane (escape hatch, optional).** Survive a group being
  *frontier-bound*: split it into independent lanes.

*(6.5d is deprioritized for now — its Build/Lab/Explain-it-back section has
been moved to the end of this document, after Phase 12.)*

The spine through all four is the **waterline**, `committed`, and it grows exactly
one term per movement: in 6.5a it just trails `claimed`; 6.5b pins it below the
lowest **open lease**; 6.5c also below the lowest **unresolved exception**; 6.5d
makes that pin **lane-scoped** and defines the contiguous `Watermark` across lanes.
Watching that one formula accrete is the through-line of the phase.

*Schema for all four movements:* `cursors(consumer_group, lane, committed,
claimed, block_hi)` (Phase 5's `consumers`/`position` renamed and extended;
`lane`/`block_hi` lie dormant until 6.5d — start with one lane, `block_hi` NULL);
a `leases` table (6.5b); a **sparse** `deliveries` collapsed to
`ready | inflight | dead` (6.5c).

---

### 6.5a — Claim-from-log: the happy path

**Concept:** the cursor was the happy path all along. With one worker and no
failures you never write a delivery row — a contiguous range is claimed,
processed, and the waterline trails right behind it. This movement alone delivers
the headline throughput result; everything after it is about surviving what goes
wrong.

**Build:**
- [x] **Evolve the cursor (a rename + two adds).** `consumers` → `cursors`;
      Phase 5's `position` becomes `claimed` (the read frontier) and you add
      `committed` (the waterline — every offset ≤ it is resolved):
      `cursors(consumer_group, lane, committed, claimed, block_hi)`. Start with one
      lane, `block_hi` NULL (those two columns wake up in 6.5d).
- [x] **Claim a range, not a row.** In one statement advance the read frontier and
      capture the window:
      `UPDATE cursors SET claimed = LEAST(claimed + $batch, head) WHERE … AND
      claimed < head RETURNING old.claimed AS lo, new.claimed AS hi;` then read
      `events WHERE "offset" > lo AND "offset" <= hi`. **No per-event row is
      written.** (`head` = `max("offset")`; PG18 has `old`/`new` in RETURNING.)
- [x] **Advance the waterline (inline, for now).** With one synchronous worker and
      no failures, set `committed = hi` once the batch is processed. (6.5b/6.5c turn
      this into a lazy roller pinned below the lowest blocker — there are none yet.)

**Lab:**
- [x] **Beat the Phase 6 baseline.** Re-run the Phase 6 throughput lab against the
      claim-from-log path on the same backlog, **at fail-rate 0**. Record the ratio
      — you're looking for the several-× the benchmark saw, *and* for `deliveries`
      staying **empty** (the happy path wrote no rows).

**Explain it back:**
1. Phase 6 wrote one row per (group, message); this movement writes none. Where,
   exactly, did the write amplification go — and what now carries the "this offset
   succeeded" fact instead of a row?
Answer: The cursor now carries the successfull messages via the committed waterline. Anything past this waterline can be considered in a terminal state.
2. What do `claimed` and `committed` each mean, and — in this single-worker,
   no-failure happy path — how do they relate? (The gap between them only opens in
   6.5b/6.5c; you'll revisit what lives in it there.)
Answer: Anything before committed is considered in a terminal state (success only right now). Anything between committed and claimed can be considered 'in-flight'. And anything past claimed can be considered 'waiting'.

**Done when:** claim-from-log beats the Phase 6 per-row baseline on the same
backlog (with numbers) and `deliveries` stayed empty, NOTES.md,
`git tag phase-6.5a`.

*→ Reference (after you've built it): `reference/waterline/pglog.go` — `Claim` (the
`reserve` CTE with `old`/`new` RETURNING) and `readRange`. Ignore the lease INSERT
and routing predicate for now; they belong to 6.5b and Phase 7.*

---

### 6.5b — Lease the range: crash recovery

**Concept:** the 6.5a happy path has a hole — a worker that crashes *after*
claiming a range but *before* finishing leaves those offsets claimed-but-
unprocessed, with nothing to reprocess them and a waterline that would falsely sail
past. The fix is a **lease** per claimed range: Phase 2's visibility timeout, but
over a *range* instead of a row. A crash now leaves an expired lease another worker
reclaims and re-reads.

**Build:**
- [x] **Lease the range (crash safety).** Insert a `leases(consumer_group, lane,
      lo, hi, lease_until, lease_token, reclaims)` row at claim time, in the **same
      transaction** as the `claimed` advance.
- [x] **Reclaim before Claim.** Workers try **Reclaim** first: grab one expired
      lease with `FOR UPDATE SKIP LOCKED` and **rotate its token** (so the original
      slow worker's later commit becomes a no-op), then re-read the exact range. A
      crashed range therefore drains before new work.
- [x] **Pin the waterline below open leases.** `committed`'s advance gains its first
      real blocker: `committed = GREATEST(committed, LEAST(min open-lease lo,
      claimed))`. Now the gap `(committed, claimed]` holds the in-flight ranges.
      Make this a **lazy** roller off the hot path — staleness only delays GC.

*(The poison-batch cap — a range whose processing keeps **crashing the worker**
would be reclaimed forever — needs somewhere to quarantine those offsets, so it's
deferred to 6.5c where the exception window gives them a home. `reclaims` counts
toward it here.)*

**Lab:**
- [x] **Crash mid-range, recover.** `--crash-after` inside a claimed range; confirm
      **no exception rows were written**, the lease expires, **Reclaim re-reads the
      exact range** and reprocesses it (at-least-once → idempotent processing).
      Watch `committed` stay pinned at the range's `lo` until the reclaim completes.

**Explain it back:**
1. A worker crashes mid-range. Walk the recovery step by step. Why does the
   reclaimer **rotate** the lease token, and what goes wrong if it merely refreshes
   `lease_until`?
Answer: Worker crashes mid-range -> Lease is 'lost' -> Lease expires -> worker reclaims on new claim cycle. Just bumping lease_until means we still have the wrong token owner so the worker does not own that claim anymore
2. What does an open lease do to `committed`, and why must it — what breaks in 6.5a
   if the waterline advances past an in-flight range?
Answer: An open lease prevents committed from moving past its low range. If we advanced committed past the leases low then we can no longer reclaim a lease if worker crashed mid lease.

**Done when:** a mid-range crash is reclaimed and reprocessed with no lost or
duplicated *effect*, `deliveries` still empty, NOTES.md, `git tag phase-6.5b`.
Must hold **R5**: Reclaim is one atomic `FOR UPDATE SKIP LOCKED` + token rotation,
so a stale worker can't free a live lease.

*→ Reference: `reference/waterline/pglog.go` — `Reclaim` (note the single-statement
token rotation and `reclaims` counter) and the `leases` INSERT inside `Claim`.*

---

### 6.5c — The exception window: park only failures

**Concept:** so far every offset either succeeds or gets reprocessed wholesale. Now
handle a message that *fails* (bad payload, downstream error) without dragging its
whole range down. After processing a range, **park only the failed offsets** as
sparse `deliveries` rows; the successes vanish (they were never rows). Parked rows
get the Phase 2 retry/backoff/dead machine — collapsed to `ready | inflight | dead`,
because *success is a DELETE* (pop-delete), so there's no `done`/`acked` state.

**Build:**
- [x] **Sparse `deliveries`, three states.** `ready | inflight | dead` (`inflight`
      is Phase 2's `processing`, renamed; **no `done`**). A row exists only for a
      failed offset; `dead` is the per-group DLQ, retained below the line.
- [x] **Commit: free the lease first, then park.** In one transaction, **free the
      lease guarded by your token** — if it's gone (you were reclaimed), park
      *nothing* and bail; only if you still held it do you `INSERT` a `deliveries`
      row (`state='ready'`) per failed offset. Use `ON CONFLICT DO UPDATE` so a
      re-park advances `attempts` toward `dead` (never `DO NOTHING`, which freezes
      attempts), and never clobbers a leased or already-`dead` row.
- [x] **Exception drain = pop-delete.** A separate worker claims `ready` exceptions
      → `inflight` (`SKIP LOCKED`), folding in expired-`inflight` reclaim (a crashed
      exception worker, like Phase 2). Run the Phase 2 machine: on success **DELETE
      the row** (no `done`); exhausted retries → `dead`. A process-crashing poison
      row can't reach user-code Nack, so reap expired-`inflight`-at-max-attempts to
      `dead` *without* user code (the crash-loop backstop).
- [x] **Quarantine the poison batch (from 6.5b).** Wire 6.5b's reclaim cap: after N
      reclaims, a happy-path range that keeps crashing the worker is parked into
      this window (per-message isolation), where the crash-loop backstop
      dead-letters the actual poison.
- [x] **Waterline gains its second term.** `committed = GREATEST(committed,
      LEAST(min open-lease lo, min unresolved-exception offset − 1, claimed))`.
      `dead` rows do **not** block. Still lazy, still off the hot path.

**Lab:**
- [x] **Watch the waterline lag and catch up.** With one failing message in a range,
      watch `committed` pin just below it while `claimed` runs ahead; let the
      exception exhaust its retries to `dead` and watch `committed` jump past it to
      the head. Confirm `deliveries` holds only the failed offset(s), never
      successes.

**Explain it back:**
1. Why must `Commit` free the lease **before** parking exceptions (and check it
   still owns it)? What does a slow/reclaimed worker inject if it parks first?
2. Why is there no `done`/`acked` state? When a happy-path message succeeds, what
   row changes — and when an *exception* succeeds, what row changes?
3. What sits in the gap `(committed, claimed]` now — and why is it *not only* the
   failed/in-flight work? (Hint: the already-succeeded offsets stranded above the
   lowest blocker — head-of-line blocking.)

**Done when:** a failing message parks one exception row, retries with backoff,
dead-letters, and the waterline pins then jumps past it; the pop-delete drain and
crash-loop backstop are demonstrated; `deliveries` stays sparse, NOTES.md,
`git tag phase-6.5c`. Must hold **R3** (free-lease-first, token-guarded Commit — no
phantom rows from a stale worker) and **R6** (`Nack`/`DeadLetter` specified; the
park `DO UPDATE` advances `attempts` toward `dead` and never clobbers a leased/dead
row).

*→ Reference: `reference/waterline/pglog.go` — `Commit` (free-first, then
`ON CONFLICT DO UPDATE` park), `ClaimExceptions` + `reapExpiredSQL` (drain with
crash-loop reap), `Ack`/`AckBatch` (pop-delete), `Nack`/`DeadLetter`, and `Advance`
(the two-blocker `LEAST`). Note `eventsFor`: the drain pairs events to deliveries
**by offset**, not row position — `UPDATE … RETURNING` order is not offset order.*

---

**The aha (the whole phase):** the cursor was the happy path all along (Phase 4);
Phase 6's per-row table was the detour. By writing a row **only on failure**, you
pay the lifecycle cost for the ~1% that needs it and let a single integer
(`committed`) speak for the 99% that just worked. The gap `(committed, claimed]` is
everything at or above the lowest unresolved offset — head-of-line blocking — which
is exactly why one hot lane can stall and why 6.5d shards it into independent lanes.

**The six load-bearing invariants:** the reference's first draft had six real bugs
an adversarial review caught and a Postgres harness confirmed. They're distributed
across the movements above — **R5** in 6.5b; **R3, R6** in 6.5c; **R1, R2, R4** in
6.5d — and written up in `bench/scale/waterline_design_v2_hybrid.md`. After you've
built all four, check your design against that list: stale-token commits,
lane-scoped Advance, frozen blocks, crash-loop dead-lettering.

**Real systems:** Kafka **offset commit** (one integer per poll — the cursor — is
the "ultimate batched ack" from Phase 3.5); SQS **visibility timeout** (the lease,
over a message; here over a range); Pulsar managed cursors. The sparse exception
table is the principled version of Kafka's "retry topic."

---

## Phase 7 — Routing

**Concept:** producers don't address consumers; they publish with attributes, and
bindings decide who receives.

**Build:**
- [x] `message_log` gets a `routing_key text` column.
- [x] `bindings(consumer_group text, pattern text, display text)` — a group
      with **no** binding matches all events; a group **with** a binding only
      receives events whose `routing_key` matches `pattern` (a **true
      wildcard**: `*` matches any run of characters, any depth — translated
      to a POSIX regex). Routing is a **predicate evaluated at read/fan-out
      time**, and it lands differently in the two models:
  - *Per-row (Phase 6):* it gates row creation — only materialize a `deliveries`
    row for groups whose binding matches.
  - *Claim-from-log (Phase 6.5):* the cursor still advances over the **whole** log
    (so `committed` stays a dense frontier), but the range read is **filtered** to
    matching events — a non-matching offset is "resolved" with no work and no
    exception row. A binding added *after* events exist therefore only affects
    offsets at/above the group's current frontier; replay to route history.
- [x] Scoped down to one matcher style (a single greedy `*`) on purpose —
      NATS-style selectors (`*` = exactly one dot-delimited token, `>` =
      one-or-more trailing tokens) let you pin an exact hierarchy depth; a
      true wildcard can't (`orders.*.central1` also matches
      `orders.us.high.central1` — there's no way to say "one segment here,
      not more"). Simpler to build and reason about; revisit only if
      bindings actually need that depth precision (tracked in TODO.md).
      Header/content matching (`headers @> '{...}'` JSONB containment) is a
      separate real alternative some systems offer (see Real systems below)
      but was cut too, for the same reason — see optional Phase 7b.

*→ Reference (after you've built it): `reference/waterline/routing.go` — see
`natsToRegex` for a NATS-style `*`/`>` → POSIX-regex translation (the
reference builds the depth-precise version this phase deliberately
simplifies away), and `readRange` in `pglog.go` for how the binding predicate
is pushed into the read so a no-binding group matches all. The reference also
builds a header/content matcher (`kind='header'`, JSONB containment) — see
optional Phase 7b.*

**Lab:**
- [x] Publish `orders.eu.created`; a group bound to `orders.*.created` receives
      it, one bound to `payments.*` does not. Routing works without the
      producer knowing any consumer exists.

**The aha:** routing is just a predicate evaluated at fan-out time. RabbitMQ's
"exchanges" are this; the flexibility lives in the matcher.

**Explain it back:**
1. Where does the routing decision execute, and why there rather than at claim
   time or produce time? What changes if a binding is added after events exist?
2. What can a depth-precise selector (NATS-style `*`/`>`) express that a true
   wildcard can't — and does this system's routing actually need that?

**Done when:** lab passes, NOTES.md, `git tag phase-7`.

**Real systems:** RabbitMQ exchanges — direct/**topic**/**headers**/fanout (topic
patterns are depth-precise, `*`/`#`, closer to NATS than to this phase's true
wildcard); NATS **subjects** with `*` and `>` wildcards; Pulsar regex
subscriptions.

---

## Phase 8 — Operational layer

**Concept:** what makes it survivable in production and observable enough to trust.

**Build:**
- [ ] **Retention policy:** a janitor that drops `events` older than X (and their
      `done` deliveries). Learn it the cheap way with native time-partitioning
      (`PARTITION BY RANGE (created_at)`) so retention is a partition `DROP`, not
      a mass `DELETE`. **This is the real home of Phase 3.5's deferred lever 3
      (archive terminal rows):** in the log model "don't let the table rot" is a
      time-based retention/partition-drop, not a mid-stream archive — same concern,
      correct mechanism for the topology. (In the *per-row* Phase 6 model, `done`
      `deliveries` rows accumulate and are the one place the original archive idea
      still applies; after the Phase 6.5 refactor the exception window self-cleans —
      success is a DELETE — so only `dead` DLQ rows persist and need a retention
      policy of their own.)
- [ ] Optionally implement **log compaction** (keep only the latest event per
      `partition_key`) to see Kafka's compacted-topic idea.
- [ ] **Observability:** expose backlog/lag per group (`head − committed`, the
      waterline gap), the `ready` exception count (retry depth), DLQ size (`dead`
      count), and oldest-unacked age. These four numbers are how you operate any
      queue.
- [ ] **Latency (optional):** add `LISTEN/NOTIFY` so producers wake idle workers
      instead of relying on poll interval, with a fallback poll for missed
      notifies and delayed (`run_at`) messages. Knowing *why* you keep the
      fallback poll (NOTIFY is fire-and-forget, lost if no listener) is the lesson.

*→ Reference (after you've built it): `reference/waterline/compaction.go` —
`Compact`/`CompactSafe` implement keep-latest-per-`partition_key` with tombstones,
and a watermark-safe floor so compaction never drops a value a consumer hasn't
passed (nor an event a live/dead `deliveries` row still references). Retention by
partition-drop and the four observability numbers (lag = `head − committed`, DLQ
size, ready count, oldest unacked) are left for you to add.*

**Lab:**
- [ ] Drop a retention partition and confirm consumers past that point are
      unaffected — and decide what happens to a consumer whose cursor is
      *inside* the dropped range (this is the Phase 5 question coming home).
- [ ] Use the harness (`--fail-rate`, `--sleep`, `--crash-after`) to induce
      every failure mode you've built and watch each of the four metrics react.
      If a failure doesn't move a metric, you have a blind spot.

**Explain it back:**
1. Why is partition-drop retention so much cheaper than `DELETE WHERE
   created_at < X`? (Think WAL, vacuum, indexes.)
2. Why must `LISTEN/NOTIFY` keep the fallback poll? Name both message classes
   it would otherwise lose.
3. For each of the four metrics: which failure mode is it the early warning for?

**Done when:** every induced failure is visible in a metric, NOTES.md,
`git tag phase-8`. Core platform operability is done — FIFO ordering (Phase 12)
and lane sharding (6.5d) are optional add-ons from here, not prerequisites.

**Real systems:** Kafka `retention.ms` + **log compaction**; consumer **lag**
monitoring (Burrow); RabbitMQ queue-depth alarms; Pulsar tiered storage.

---

## Phase 9 — Consumer fault isolation & recovery

**Concept:** `consumerFunc` is a black box you don't control — it can return an
ordinary error, panic, hang forever, or the datastore call around it can blip.
Only the first case is handled today, through the retry/backoff/dead path built
in 6.5c. This phase closes the other three, one failure mode at a time, using
that same exception window as the landing zone for each — so a panic or a hang
becomes "one message failed," never "the whole range/consumer died." No
`reference/waterline` counterpart for this phase — the reference implements only
the datastore/`Log` layer (`Claim`/`Commit`/`Advance`/…), never the worker loop
that calls `consumerFunc`, so there's nothing to compare against here; this
phase is `pkg/consumer`-specific.

**Build:**
- [ ] **Datastore-blip retry backoff.** `Consume`'s `Project`/`Process`/
      `RollWaterline` goroutines currently return a raw DB error straight to the
      errgroup, killing the whole consumer on a transient network blip. Wrap
      their DB-facing calls in a retry-with-backoff instead of propagating
      immediately.
- [ ] **Graceful-shutdown lease truncation.** `Consume` already calls a
      pluggable `p.ShutdownFunc` on the way out (`Shutdown`, `consumer.go`), so
      this is extending that hook, not building shutdown from scratch: before
      releasing an in-flight lease, update its low bound to the last
      successfully processed offset in that range, so a restart doesn't
      reprocess work that already completed.
- [ ] **`recover()` around the `consumerFunc` call.** A bare Go panic (nil map
      write, index out of range, a bad type assertion on an unexpected payload)
      is indistinguishable from a real process crash today — it takes down the
      *whole claimed range* (lease expires, gets reclaimed, re-reads the exact
      same range, panics again) instead of failing the one message. `consumerFunc`
      is currently called raw, with no recover, at **three independent sites**:
      `CursorClaim`, `ExceptionClaim`, and `LifecycleClaim` — so this is a good
      candidate for one small shared helper (e.g. `callSafely(ctx, consumerFunc,
      work) error`) all three call, instead of tripling the defer/recover logic.
      A recovered panic becomes an ordinary error routed through the **same**
      per-message retry/backoff/dead path as any other failure, with the panic
      value + `runtime/debug.Stack()` captured into `last_error`. Note what
      `recover()` does *not* catch: OS-level faults (stack overflow, SIGSEGV via
      cgo, OOM-kill, an external kill) — those still need the range-level
      quarantine cap (6.5c) as the backstop.
- [ ] **Hard per-message timeout via a detached-goroutine race.** `WorkTimeout`
      is validated today but never enforced as a deadline — it only feeds the
      lease-duration math (an *earlier* version of this code did wrap the call in
      `context.WithTimeout`, still visible commented-out around `consumer.go`'s
      dead V1/V2 paths, but that wrapping was dropped and never replaced). A bare
      `context.WithTimeout` isn't enough on its own anyway: it's cooperative
      (just closes `ctx.Done()`), so it does nothing for a call that never checks
      its context (blocking cgo, a tight CPU loop, a library that ignores ctx).
      Instead race completion against a timer with a buffered channel so the
      *caller* can give up independent of the callee — and fold this into the
      same shared helper as the `recover()` item above, since both wrap the exact
      same three call sites:
      ```go
      done := make(chan error, 1) // buffered so a late send never blocks
      go func() { done <- consumerFunc(ctx, &work) }()
      select {
      case err := <-done:
          // finished in time
      case <-time.After(p.Config.WorkTimeout):
          err = ErrWorkTimeout
          // consumerFunc's goroutine is NOT killed — it's abandoned, not stopped
      }
      ```
      Go has no goroutine kill: this converts "one message hangs the whole
      range, worker gets externally killed, whole range reclaimed and
      re-crashes" into "one message leaks one goroutine" — better containment,
      not a full fix. Route the timeout through the retry/dead path like any
      other failure. If reusing `errgroup` for structural consistency, do NOT
      call `Wait()` synchronously on this — `Wait()` blocks until every
      goroutine returns, so it hangs right alongside a genuinely stuck call.
- [ ] **Track abandoned goroutines.** Once calls can be abandoned, a small
      in-process registry — keyed per **(message, attempt)**, not per-message,
      since the same message can time out on more than one attempt and a
      per-message key would let the second overwrite the first's entry. Expose
      a counter (`hard_timeouts_total` — how often this happens), a gauge
      (`abandoned_outstanding` — how many are leaked *right now*, the direct
      leak-prediction signal), and a histogram (late-finish latency, for the
      ones that do eventually return). Tag `last_error` distinctly for a
      hard-timeout abort (e.g. `"hard timeout after Ns, goroutine abandoned"`)
      so it's queryable without extra infra.
- [ ] **(Stretch, low priority) lease heartbeat/renewal.** For long-running jobs
      whose runtime legitimately exceeds `WorkTimeout` but still want fast
      reclaim on a real crash: hand `consumerFunc` a `heartbeat()`/`touch()`
      handle; the lease only extends when touched
      (`UPDATE ... SET lease_until = now()+ext WHERE id=$1 AND lease_token=$2`).
      `RowsAffected==0` on a renew means the lease was already reclaimed —
      cancel the work context. Keep a hard max-duration ceiling regardless, so
      a buggy progress loop that keeps touching forever still eventually caps
      out. Opt-in; short jobs ignore it and rely on the fixed lease window.

**Lab:**
- [ ] Induce each failure mode with the existing harness (or new flags): a
      `consumerFunc` that panics, one that hangs past `WorkTimeout`, and a
      forced DB error mid-loop. Confirm: the panic retries/dead-letters just
      the one message, not the whole range; the hang times out and retries
      while the abandoned goroutine shows up in the gauge; the DB blip doesn't
      kill the consumer and it recovers once the blip clears.

**Explain it back:**
1. Why does a recovered panic have to go through the *exact same*
   retry/backoff/dead path as an ordinary error, instead of its own
   special-cased handling?
2. Why is `context.WithTimeout` alone insufficient to enforce `WorkTimeout`,
   and what does the detached-goroutine race actually buy you given Go has no
   goroutine kill?
3. Why key the abandoned-goroutine registry by (message, attempt) rather than
   by message alone?

**Done when:** all three induced failure modes recover correctly and are
demonstrated in the lab, NOTES.md, `git tag phase-9`.

**Real systems:** this is the same isolation problem every worker framework
solves — Sidekiq's `retry` + `death_handlers`, Temporal's activity heartbeats
(the model for the stretch lease-heartbeat item), Kubernetes liveness probes as
the outer backstop for faults nothing in-process can catch.

---

## Phase 10 — Observability: logging & the rollup model

**Concept:** right now the only way to see consumer health is to query
Postgres directly. This phase adds the operator-facing surface — a pluggable
logger and a live debug readout — and settles the lazy-vs-synchronous waterline
question this plan deliberately deferred back in 6.5b ("make this a lazy roller
off the hot path — staleness only delays GC"). No `reference/waterline`
counterpart either: it has no logger and no debug surface of its own (it's a
teaching artifact meant to be read, not operated), so there's nothing to
compare against — you're building past what the reference bothered with.

**Build:**
- [ ] **Common logger interface.** The internal `pkg` logging should accept a
      logger *interface*, not a hardcoded implementation, so callers can plug
      in their own structured or unstructured logger.
- [ ] **Writer-based default logger.** Provide a default implementation that
      takes an arbitrary `io.Writer`, so a caller with no opinions gets
      something reasonable for free.
- [ ] **Debug/metrics readout.** A debug mode or method that prints current
      queue state on demand: the `claimed`/`committed` gap per group, exception
      counts (`ready`/`inflight`/`dead`), and open-lease count — an ad hoc,
      inspectable version of Phase 8's four operational numbers, useful before
      you've wired up a real metrics backend.
- [ ] **Resolve the lazy-vs-synchronous rollup.** Decide, with measured
      numbers, whether `AdvanceWaterline` should stay a periodic lazy tick or
      become synchronous — advance right after a lease/batch resolves instead
      of waiting for the next tick. Record the latency-vs-overhead tradeoff you
      measured, not just the decision.
- [ ] **Inflight/completed visibility.** Surface, per group, how many
      messages/batches are currently inflight vs. resolved — the metric gap the
      lazy-rollup question originally flagged (today you can infer this from
      `claimed − committed`, but it's not exposed as a first-class number).

**Lab:**
- [ ] Run the consumer under load with the new debug output on; watch the
      `claimed`/`committed` gap and exception counts move in real time as you
      inject failures with the existing harness (`--fail-rate`, `--crash-after`).
      Compare how quickly `committed` reflects reality under the lazy roller vs.
      the synchronous option.

**Explain it back:**
1. What's the tradeoff between a lazy periodic rollup and a synchronous one —
   what do you gain and what do you pay for each?
2. Why does a live debug readout of claimed/committed/exception-count matter
   even though the underlying data was always queryable in Postgres directly?

**Done when:** the pluggable logger and debug readout work end-to-end, the
lazy-vs-synchronous rollup decision is made and recorded with measured
reasoning, NOTES.md, `git tag phase-10`.

**Real systems:** Kafka consumer-group lag exporters; the `slog` interface
pattern (Go's own answer to "pluggable logger"); Temporal/Sidekiq dashboards
built on exactly these counters (backlog, in-flight, dead-letter, oldest-unacked).

---

## Phase 11 — Architecture cleanup: datastore boundary & producer API

**Concept:** a few structural seams accumulated while building the platform —
the consumer knows more about Postgres-specific datastore internals than it
should, the producer can't fan out to multiple queues in one transactional
write, and a couple of small polish items were deliberately deferred until the
core was stable. This phase is cleanup, not new capability.

*→ Reference (partial, for the first item only): `reference/waterline/types.go`
defines a deliberately small `Log` interface (`Claim`/`Reclaim`/`Commit`/
`ClaimExceptions`/`Ack`/`Nack`/`DeadLetter`/`Advance`/`Watermark` — 9 methods) as
the target shape to compare `pkg/consumer`'s current `Datastore` interface
against (`datastore.go` — 13 methods, since it supports **both** `CURSOR` and
`LIFECYCLE` modes behind one interface, which the reference deliberately keeps
separate — see the "honest delta" note near the top of this document). One
caveat worth knowing before you audit: the reference doesn't fully solve
backend-agnosticism either — `Range.Token` is typed `pgtype.UUID` directly in
its public struct, the same pgx-specific leak `Datastore.Commit`'s `token`
parameter has. So "abstract the boundary" here means making a *deliberate,
documented* choice about how far to go, not necessarily eliminating every pgx
type — even the reference didn't bother.*

**Build:**
- [ ] **Abstract the datastore boundary.** Audit `pkg/consumer` for places that
      assume more than the `Datastore` interface promises, so swapping the
      backing store wouldn't require touching consumer logic.
- [ ] **Evaluate `database/sql` vs. `pgx`.** Decide whether removing the
      `pgx`-specific dependency is worth losing pgx-only features (native
      types, `COPY`, etc.) — document the decision even if the answer is "keep
      pgx."
- [ ] **Multi-target transactional enqueue.** Extend the producer so one call
      can publish to multiple queues/topics within the same transaction
      (today's `Produce(ctx, tx, work)` only targets one).
- [ ] **Attempt audit trail.** An append-only table recording each attempt
      (`attempted_at`, error) per message, written but **never read by the hot
      path** — so it can't slow down claims — for debugging/auditing only.
- [ ] **Small polish.** Swap `context.WithTimeout`/`WithDeadline` for their
      `*Cause` variants where it improves error messages; batch `Commit`'s
      per-row exception-park `INSERT`s via `pgx.Batch` instead of a loop (this
      was deferred on purpose back in 6.5c — revisit now that the surrounding
      exception machinery is stable).

**Lab:** no new failure mode to induce — this is refactor-and-verify. Re-run
the full existing lab suite (`just reclaim-lab`, `just exception-lab`, and the
rest) after the datastore-boundary refactor and confirm no regressions.

**Explain it back:**
1. What did depending directly on Postgres-specific datastore internals cost
   you, concretely — name one place in the consumer that would have to change
   if you swapped datastores?
2. Why does the attempt-audit table deliberately never get read by the hot
   path?

**Done when:** the datastore boundary is audited and cleaned up (or documented
as intentionally not fully abstracted), multi-target enqueue works, all
existing labs still pass, NOTES.md, `git tag phase-11`.

**Real systems:** the repository-pattern debate in general; River/Oban both
commit to a single backing store rather than abstracting it — worth deciding
deliberately which side of that tradeoff this project is on.

---

## Phase 12 — Optional FIFO partitions

**Concept:** ordering on demand, paid for only where you opt in.

**Build:**
- [ ] Add `partition_key text` to `events` (nullable = no ordering).
- [ ] **Decide which path carries ordering.** The bare claim-from-log happy path
      (Phase 6.5) is *unordered* under concurrent workers — it hands out contiguous
      ranges in parallel, so two workers can process two offsets of the same key at
      once. FIFO is therefore an **opt-in on the lifecycle path**: a keyed stream
      materializes `deliveries` rows and drains them with the keyed claim below;
      unordered, max-throughput streams stay on the bare cursor.
- [ ] **Cursor consumers (the trivial case):** a *single* reader in `offset` order
      already gives per-partition order — the K=1 happy path. Keeping order under
      *many* workers is the interesting case, and it needs the keyed claim below.
- [ ] **Lifecycle (delivery) consumers:** enforce "at most one in-flight per
      key" — claim with a predicate that **skips rows whose `partition_key`
      already has an in-flight delivery in this group**. Null key → no
      constraint, full concurrency.

*→ Reference (after you've built it): `reference/waterline/partitions.go` —
`ClaimPartitioned` is the keyed claim. Note the extra subtlety it solves that this
phase's Explain-it-back asks about: to keep order **through a retry**, only the
lowest unresolved offset of a key is eligible, so a backed-off head blocks its
later offsets (and a dead head stops blocking).*

**Lab:**
- [ ] Messages with `partition_key='acct-42'` always process in order even under
      50 workers; messages with no key parallelize fully. Both on the same
      stream. Make the consumerFunc print `(key, seq)` so order violations are
      visible at a glance.
- [ ] **Hot-key demo:** send 1000 messages all keyed `acct-42` alongside 1000
      unkeyed; watch the keyed stream serialize to single-worker throughput
      while the unkeyed stream saturates all workers.

**The aha:** ordering and concurrency trade off — a hot key serializes — so you
make it *optional* per message. The practical resolution of the
"FIFO vs concurrency" problem.

**Picks up from Phase 3.5 (deferred lever 4):** `partition_key` is also the
principled answer to the claim-hotspot contention you measured back in Phase 3.5.
Sharding the claim by key spreads workers across distinct index ranges instead of
all contending on one hot end — the throughput fix and the ordering primitive turn
out to be the same mechanism.

**Explain it back:**
1. Why can't you have both total ordering and full concurrency? Where exactly
   does the serialization happen in your claim query?
2. What does a retry (Phase 2 backoff) do to per-key ordering? Is "at most one
   in-flight per key" enough to preserve order through a retry?

**Done when:** labs pass, NOTES.md, `git tag phase-12`.

**Real systems:** Kafka **partitions** (key → partition, order within
partition); Pulsar **Key_Shared** subscriptions (per-key order across multiple
consumers); SQS FIFO **MessageGroupId**.

---

## 6.5d — Shard the hot lane (deferred, optional)

*Deprioritized for now — not part of the current focus (Phases 7–11). Revisit
only if a single group's frontier is provably contended. Originally the fourth
movement of Phase 6.5; kept labeled `6.5d` rather than folded into the main
numbered sequence, since it's still conceptually part of that refactor.*

**Concept:** a single group's frontier is one `cursors` row, so many concurrent
workers contend on it. Give the group **K lanes**, each owning a **frozen,
contiguous block** of the log, and let them drain independently. Keep K=1 until a
group is *provably* frontier-bound — don't shard speculatively. This is the
optional capstone; skip it unless you want to feel the contention and lift it.

**Build:**
- [ ] **Freeze K contiguous blocks.** From a frozen head H, seed K `cursors` rows,
      lane `s` owning block `(H·s/k, H·(s+1)/k]` via `block_hi`. The claim caps at
      the lane's `block_hi` instead of `head`:
      `LEAST(claimed + $batch, COALESCE(block_hi, head))`. Frozen + contiguous ⇒
      lanes never overlap (no dup) and never leave a seam (no gap).
- [ ] **Lane-scope the exception blocker.** `Advance`'s exception term must be
      `AND lane = $lane`, or one lane's stuck exception freezes every lane's
      waterline.
- [ ] **Define the contiguous `Watermark`.** The group's cumulative guarantee is the
      `committed` of the **first lane not yet at its `block_hi`** (everything below
      it is dense), not `min(committed)` (which sticks once lane 0 finishes, so
      `CaughtUp` could never fire).

**Lab:**
- [ ] **(Optional) sharding.** One hot group, single lane vs K lanes; plot the
      frontier throughput. Confirm `Watermark` reaches head only when *all* lanes
      drain their blocks.

**Explain it back:**
1. When would you shard a single group into lanes, and what does that trade away?
2. Why is striping by `offset % K` the wrong way to do it — what can't a dense
   single-integer cursor represent?
3. Why is `Watermark` the first-incomplete-lane's `committed` and not
   `min(committed)` across lanes?

**Done when:** K lanes beat a single lane on a frontier-bound group (with numbers)
and `Watermark` reaches head only when all lanes drain, NOTES.md,
`git tag phase-6.5d`. Must hold **R1/R4** (lane claims clamp to a **frozen**
`block_hi` — no gap/dup across lanes) and **R2** (lane-scoped `Advance` — one lane's
exception can't freeze the others).

*→ Reference: `reference/waterline/sharding.go` — `InitLanes` (frozen-block seeding,
the re-shard and too-small guards) and `CaughtUp`; `pglog.go` `Watermark` (the
first-incomplete-lane query).*

---

## 7b — Header/content routing (deferred, optional)

*Deprioritized for now — cut from Phase 7 to keep the first pass simple: one
matcher style is enough to learn where the routing predicate lives and how it
differs between the CURSOR and LIFECYCLE paths, and this is a pure addition
on top of that (nothing built in Phase 7 needs to change). Revisit only if a
real routing need can't be expressed as a `routing_key` pattern.*

**Concept:** `routing_key` matching is positional/hierarchical
(`orders.eu.created`, matched left-to-right by token). Some routing
decisions are about an *unordered set of attributes* instead — "region=eu
AND tier=gold, regardless of what else is on the event" doesn't fit a
hierarchy at all. A header/content matcher answers that case with JSONB
containment instead of a regex.

**Build:**
- [ ] Add `headers jsonb not null default '{}'` to `message_log`.
- [ ] `bindings` needs a way to carry a header-match alongside the existing
      `pattern` column — reintroduce a discriminator (a `kind` column or
      similar) once there are two matcher shapes to choose between; Phase 7
      dropped it because with only one shape it had nothing to discriminate.
- [ ] Header matcher: `headers @> '{...}'` (JSONB containment) — a group
      matches when its bound match object is a subset of the event's headers.
- [ ] **The foot-gun to guard against:** an empty `{}` header match is `@>`-true
      for *every* event, silently widening a group to match everything.
      Reject it at bind time.

*→ Reference: `reference/waterline/routing.go`'s `BindHeader` (the foot-gun
guard) and the `kind='header'` branch of `pglog.go`'s `readRange` — this was
already built there in full; port it once you want it here.*

**Lab:**
- [ ] Bind a group to `{"region":"eu","tier":"gold"}`; only events whose
      headers are a superset of that match route to it. A topic-bound group
      and a header-bound group coexist fine on the same stream.

**Explain it back:**
1. Topic-style vs header-style matching — when is each the right tool, and
   what routing question can't a single `routing_key` string express?
2. Why must an empty header match be rejected instead of silently matching
   everything?

**Done when:** lab passes, NOTES.md, `git tag phase-7b`.

**Real systems:** RabbitMQ header exchanges (`x-match: all`/`x-match: any`)
— the topic-vs-headers split Phase 7 cites via RabbitMQ exchanges is exactly
the one this phase revisits.

---

## How to use this

- **Do the phases in order.** Resist jumping to the synthesis (Phases 6–6.5).
  Each checkpoint is a concept you can't skip.
- **The labs are the learning.** Reading the aha is not the same as watching
  two workers not collide. If you skipped a lab, the phase isn't done.
- **Explain-it-back is the retention mechanism.** Answer from memory. Wrong or
  blank answers mean re-run the lab, not re-read the plan.
- **Tag every phase.** The diffs between tags are a record of *why* each
  refactor happened — that's the document your future self wants.
- **Stop when it's enough.** Phases 1–3 alone are a production-grade job queue.
  Phases 4–6 graduate you from "queue" to "log+queue platform"; Phase 6.5
  (claim-from-log) is the throughput payoff that makes it scale. 7–11 round out
  routing, operability, and the fault-tolerance/architecture gaps tracked in
  TODO.md — do them in order too, they build on each other the same way.
- **Current focus:** Phase 7 (Routing), then Phase 8 (Operational layer), then
  Phases 9–11 (TODO.md-derived: fault isolation, observability, architecture
  cleanup). Phase 12 (FIFO partitions), 6.5d (lane sharding), and 7b
  (header/content routing) are optional and deliberately deferred to the end
  of this document — pick them up only if a real workload demands ordering,
  lane-level throughput, or attribute-based routing a `routing_key` pattern
  can't express.
- **The meta-lesson:** by Phase 6.5 you'll understand *in your hands* why Kafka,
  RabbitMQ, and Pulsar are different — they're the same primitives with different
  foundational defaults. That understanding is worth more than the code.
