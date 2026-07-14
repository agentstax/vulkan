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
| 8c compaction | `compaction.go` — keep-latest-per-key, tombstones, watermark-safe (this project takes a different, read-time-filtering approach instead — see 8c) |
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
| 8 — Operational layer | ⬜ | retention split into **8a**, log compaction split into **8c**; observability moved to **10**, LISTEN/NOTIFY moved to **8d** |
| 8a — Retention | ✅ done | partition-drop by `RANGE (id)` (claim-path pruning) + bounded DELETE sweep for the low-volume tail; Go janitor, no pg_partman; answers in NOTES.md; tag `phase-8a` |
| 8b — Per-topic tables | ✅ done | each topic gets its own table/id sequence/partition set/janitor; `routing_key`/`bindings` kept, now scoped per topic; fixes 8a's global floor + 8c's fan-out cost; answers in NOTES.md; tag `phase-8b` |
| 8c — Log compaction | ✅ done | keep-latest-per-key, filtered at claim time; `latest_keys` O(1) index landed (write-cost measured), no schema tombstone, retention needs no compaction-awareness; answers in NOTES.md; tag `phase-8c` |
| 8d — Latency: LISTEN/NOTIFY | ⬜ | deprioritized — optional, deferred; moved to the end of this document |
| 9 — Consumer fault isolation & recovery | ✅ done | DB-blip retry (`pkg/retry`, idempotency_keys), graceful-shutdown lease truncation (`PartialCommit`), panic recovery + hard per-message timeout (`callSafely`), abandoned-goroutine tracking; found/fixed a real `pkg/retry.Wrap` bug along the way; answers in NOTES.md; tag `phase-9` |
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
- [x] **Retention policy → split out into its own movement, 8a below.** The
      design grew past a bullet: partition by `RANGE (id)` (not `created_at` —
      the claim path prunes on id), a Go janitor instead of pg_partman, and a
      hybrid drop + bounded-DELETE for the low-volume tail. **8a is the real
      home of Phase 3.5's deferred lever 3 (archive terminal rows):** in the log
      model "don't let the table rot" is time-based retention, not a mid-stream
      archive — same concern, correct mechanism for the topology. (In the
      *per-row* Phase 6 model, `done` `deliveries` rows accumulate and are the
      one place the original archive idea still applies; after the Phase 6.5
      refactor the exception window self-cleans — success is a DELETE — so only
      `dead` DLQ rows persist and need a retention policy of their own.)
- [x] **Log compaction → split out into its own movement, 8c below.** The
      design departs from Kafka's own background/segment-based compaction:
      instead of a janitor deleting superseded rows once a watermark floor
      allows it, this project filters at claim time — the log stays
      append-only and physically unmodified, and `readMessages`/`FanOut`
      only ever return the latest row per key.

*→ Reference (after you've built it): retention by partition-drop is 8a's
territory, log compaction is 8c's (where `reference/waterline/compaction.go`'s
watermark-safe background-delete approach is worth comparing against the
read-time-filtering path chosen there) — the four observability numbers
themselves moved to Phase 10, and `LISTEN/NOTIFY` latency moved to 8d.*

**Lab:** (The retention-drop lab moved to 8a with the rest of retention; the
failure-mode/metrics lab moved to Phase 10 with observability.)

**Done when:** retention (8a), per-topic tables (8b), and compaction (8c) are
done and tagged, NOTES.md, `git tag phase-8`. Core platform operability is
done — `LISTEN/NOTIFY` (8d), FIFO ordering (Phase 12), and lane sharding
(6.5d) are optional add-ons from here, not prerequisites.

**Real systems:** consumer **lag** monitoring (Burrow); RabbitMQ queue-depth
alarms; Pulsar tiered storage.

---

### 8a — Retention: partition-drop, and the low-volume hybrid

**Concept:** retention on an append-only log means old rows live forever, and
at volume a mass `DELETE WHERE created_at < X` is the wrong tool — every deleted
row is WAL'd, every index entry has to be cleaned, and vacuum inherits the debt.
The cheap mechanism is dropping a whole **partition**: one DDL statement, no
per-row work. Two design decisions make it work here, and both cut against the
"obvious" setup:

- **Partition by `RANGE (id)`, not `created_at`** — even though retention is
  time-based. Every hot query on `message_log` filters by **id** (the claim
  range `id > lo AND id <= hi`, the head `MAX(id)`, the lifecycle join
  `ON m.id = message_id`); none mentions `created_at`. Partition by time and
  the planner can't prune *any* of them — a claim probes every partition's
  index (365 daily partitions = 365 probes per claim). Partition by id and
  each hot query prunes to 1–2 partitions. Retention stays time-based anyway,
  because ids are append-ordered: partition boundaries are time-ordered too,
  so "old enough to drop" is still decidable per partition. (Bonus: Postgres
  requires the partition key inside any PK — by id, `PRIMARY KEY (id)`
  survives unchanged; by time it would bloat to `(id, created_at)`.) This is
  Kafka's segment model exactly: segments are *offset*-ranged files, and
  `retention.ms` is enforced by checking a segment's last timestamp.
- **Hybrid drop + bounded DELETE**, because fixed-width id partitions have a
  weak end: a low-volume log (say 100 msgs/day with a 90-day TTL) never fills
  a partition, so rows would sail past the TTL waiting on a drop that never
  comes. Kafka rolls segments on **size OR time** (`segment.bytes` /
  `segment.ms`), but that doesn't translate — Postgres range partitions
  declare their bounds at *creation* and can't shrink at roll time. Instead:
  drop partitions that are expired *whole*, and sweep the expired prefix of
  the oldest surviving partition with a small `DELETE`. The DELETE's cost is
  proportional to volume — cheap exactly when it's the mechanism in play; at
  high volume the drop already took everything and the sweep deletes ~nothing.
  The two mechanisms cover each other's weak ends.

Constraint honored throughout: **no extensions** (no pg_partman — adoption/
compatibility). Declarative partitioning is core Postgres; pg_partman is only
automation around `CREATE TABLE … PARTITION OF` and `DROP TABLE`, and the
janitor below *is* that automation, in Go, on a ticker.

**Build:**
- [x] Convert `message_log` to `PARTITION BY RANGE (id)` with fixed-width id
      partitions (width configurable — it's the retention granularity knob,
      sized in rows, not days). The migration creates the first partition so
      the table is insertable before the janitor ever runs; folded into
      `001_messages`, per house style.
- [x] **Janitor: create-ahead.** When `MAX(id)` nears the active partition's
      upper bound, create the next partition. A producer must never hit a
      missing partition.
- [x] **Janitor: drop.** A partition is droppable when its *newest* row is past
      the TTL. Read "newest" as `ORDER BY id DESC LIMIT 1` — rides the PK
      index, no `created_at` index needed (id order ≈ time order).
- [x] **Janitor: sweep.** Delete the expired prefix of the oldest surviving
      partition — walk `ORDER BY id ASC`, delete while `created_at < cutoff`,
      stop at the first survivor. Index-backed, cost ∝ rows actually expired.
- [x] **Waterline-safe drop floor:** never drop a partition containing ids
      above any group's `committed` (`min(committed)` across cursors), with an
      explicit config knob to opt into Kafka's "lagging consumer falls off the
      retention window" semantics instead. Either is defensible; it must be a
      choice, not an accident.
- [x] Sweep orphaned references alongside the drop: `deliveries`/exception rows
      pointing into a dropped id range would join to nothing and park forever.
- [x] **Open question, not settled:** retention today is **per-log**, not
      per-routing-key — one shared log, one TTL, and the drop floor's
      `MIN(committed)` spans every cursor regardless of what routing_key it
      actually consumes, so one lagging group on an unrelated topic blocks
      drops for everyone. Kafka avoids this because `retention.ms` is
      per-topic and each topic *is* its own log. Reconsider keeping every
      'topic' in the same `message_log` table — may need an actual topic
      concept (its own log/partitions) rather than routing_key filtering over
      a shared one. (TODO.md has the durable pointer.)

**Lab:**
- [x] Shrink the partition width and TTL to lab scale; publish across several
      partitions; prove with `EXPLAIN` that a claim touches 1–2 partitions,
      not all of them — the pruning payoff, observed rather than assumed.
- [x] Drop an expired partition: confirm consumers *past* it are unaffected,
      and a consumer whose cursor is *inside* the dropped range claims an
      empty batch and advances over the hole (the Phase 5 lag question coming
      home). Then enable the drop floor and watch the same drop get refused.
- [x] The low-volume case: a half-full partition with an expired prefix —
      confirm the sweep deletes exactly the prefix and the partition survives.

**Explain it back:**
1. Why is partition-drop retention so much cheaper than `DELETE WHERE
   created_at < X`? (Think WAL, vacuum, indexes.)
Answer: Every delete is a transactional write to the WAL which then has to be committed / flushed.
Additionally indexes have to be cleaned up as well and then of course each page has to be deleted which is pressure on the vacuum.
With partition drop none of those things happen it is just a pure disk delete.
2. Retention is time-based — so why partition by `id` and not `created_at`?
   What exactly goes wrong at claim time with 365 daily partitions?
Answer: because message_log is append only, id is approxametly time ordered. Because of that we can use id to our advantage.
Time based partitions require our table primary key to include created_at which adds write/delete overhead.
Additionally if we did time based partitions with a ttl of a year claim queries would have to scan 365 partitions which would slow down the hot path
and degrade throughput quality.
3. The hybrid reintroduces `DELETE` — why doesn't it reintroduce the problem
   partition-drop exists to avoid?
Answer: Because the sweep never touches the active, high-volume partition — SweepExpiredPartitions only walks the oldest surviving *non-active*
partition. At high volume, drop consumes whole partitions fast enough that by the time a partition is old enough to sweep, it's already been
dropped whole — the sweep finds an empty prefix, not a DELETE under load. At low volume there's no whole partition to drop yet, so the DELETE's
cost is what's paying for correctness, and it's cheap exactly because the row count is small by definition. The two mechanisms cover each other's
weak end instead of both running at once.
4. What does the drop floor protect, and what precisely happens to a consumer
   group when you turn it off and drop past its `committed`? (Kafka's
   "consumer fell off the retention window," now in your own system.)
Answer: A drop floor protects against messages not being processed. If we didn't have floor protection partitions or messages could be deleted before a
cursor / consumer group has reached them.
Precisely: nothing detects the gap. FreshClaimMessagesWithCursor advances `claimed` by pure id arithmetic against MAX(id)
(`claimed = LEAST(claimed + limit, MAX(id))`), never checking whether rows still exist in that range. The lease still gets created for
`(low, high]` and readMessages still runs its SELECT — if the partition backing that range is gone, the SELECT just returns fewer rows, even
zero, with no error. `claimed` and then `committed` advance past the hole exactly as they would for a normal batch. So a lagging group doesn't
"jump ahead" via any special-cased skip — it was always going to advance on schedule; the dropped rows just silently never get delivered, and
there's no in-band signal that it happened (only an external one, like the Phase 5 lag metric going quiet).

**Done when:** `EXPLAIN` shows claim-path pruning, a drop is refused by the
floor and permitted without it, the sweep handles the low-volume tail, all
three labs pass, NOTES.md, `git tag phase-8a`.

**Real systems:** Kafka **segments** (offset-ranged files — the partition-by-id
move; `retention.ms` checks a segment's *last* timestamp — the drop rule;
`segment.ms`/`segment.bytes` roll — the gap the hybrid fills instead);
pg_partman (the janitor, as the extension this project won't take); Timescale
`drop_chunks`.

---

### 8b — Per-topic tables: independent logs, routing stays within them

**Concept:** two problems this project already found and filed as TODOs turn
out to be the same root cause: 8a's drop floor is `MIN(committed)` across
*every* cursor sharing `message_log`, so one lagging group blocks retention
for every unrelated topic riding along in the same table; 8c's compaction
lookup has to probe every live partition up to a claim's high, because `id`
is one `BIGSERIAL` shared by every topic, diluting how densely a rarely-used
key's own writes cluster together. Both are symptoms of one table doing the
job of many logs. Kafka's fix is that a topic *is* its own log — its own
partitions, its own `retention.ms`. This phase does the same: each topic gets
its own physical table, its own id sequence, its own partition set, its own
janitor.

Two decisions this phase turns on, both settled the hard way:

- **A topic is a new, coarser concept above `routing_key` — not a
  replacement for it.** The tempting simplification is to collapse them: one
  routing_key value, one topic, one table — closer to how a Kafka consumer
  subscribes to topics by name-pattern rather than filtering records
  server-side within one topic. But `routing_key` today is free text a
  producer can invent with zero ceremony (a new tenant id, a new region), and
  a topic under this design carries real weight (its own sequence, partition
  set, retention config) that shouldn't spin into existence from a producer
  typo. Collapsing them would force a physical table for every fine-grained
  slice — real per-topic janitor overhead multiplied by however many slices
  the domain naturally has — and would throw away Phase 7's retroactive
  binding application (a binding added after messages exist still applies to
  them, as long as they're unclaimed), which only works because the log
  those messages live in is already shared. So `routing_key`/`bindings`
  survive this phase completely unchanged in mechanism, just newly scoped to
  one topic's own table instead of the single system-wide one.
- **Each topic's id sequence must be its own, not a shared global one.** A
  shared sequence looks cheaper — `message_id` stays globally unique, so
  `cursors`/`deliveries`/`leases` could keep their current keys untouched.
  It doesn't actually work: `EnsureNextPartition` decides partition
  boundaries by raw id arithmetic, and if a topic only receives a fraction of
  the system's total id throughput, "1,000,000 raw ids of partition width"
  no longer means "1,000,000 of this topic's own rows" — it means however
  many happened to fall in that range, which drifts with total system
  traffic and can't be reasoned about per topic. Worse, it makes *more*
  physical partitions accumulate per unit of a topic's real data, which
  makes 8c's fan-out problem worse, not better. A shared sequence also
  doesn't dodge propagating "which topic" into `deliveries` either — a row
  still needs to say which table its `message_id` lives in. So the honest
  design accepts the full blast radius: each topic's table gets its own
  dense sequence, and `cursors`/`deliveries`/`leases` become scoped to
  `(consumer_group, topic_id)` rather than just `(consumer_group)`.

**What this phase does *not* fully solve, on purpose:** a lagging group on
one `routing_key` slice still shares its topic's drop floor with every other
slice in that same topic — splitting by topic re-scopes the contamination
from system-wide to within-one-topic, it doesn't eliminate it. If two slices
within a topic diverge badly in consumer lag, that's the signal to split
them into separate topics, a deliberate operational choice this phase
enables rather than automates.

**Build:**
- [x] New `topics` table: `id BIGSERIAL PRIMARY KEY, name TEXT UNIQUE NOT
      NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`. The physical
      table for a topic is named `message_log_<id>` — a server-generated
      integer, never the free-text `name`, gets interpolated into dynamic
      DDL. Same discipline `EnsureNextPartition` already uses for partition
      numbers, just one level up; a hashed or raw-name table suffix was
      considered and rejected — the lookup table is needed regardless (app
      code always refers to topics by name), so its own PK is the simplest
      safe identifier available, no collision handling, no injection
      surface.
- [x] **Partition names need a second suffix, or they collide.** A
      partition of `message_log_<id>` would naturally want the same
      `message_log_<n>` pattern `EnsureNextPartition` already uses today —
      but that's the *same* naming scheme the topic table itself now uses
      (`message_log_<id>`), so topic 3's table and what would have been
      "partition 3" of some other topic are one collision away from
      colliding. Partitions need their own two-part name,
      `message_log_<topic_id>_<n>`, and `EnsureNextPartition`'s (and
      `DropExpiredPartitions`'/`SweepExpiredPartitions`'/`existingPartitions`')
      `fmt.Sprintf` templates all need updating to match.
- [x] **Topic registration is explicit, not implicit — and its UX is the
      idempotent declare (settled 2026-07-08).** Publishing to or claiming
      from an unregistered topic name is an error. Unlike a partition (which
      self-heals silently because it's cheap and consequence-free) or a
      `routing_key` value (free text, no schema weight), a topic carries
      real resource cost — creation never happens as a side effect of a
      produce/claim call. The shape, picked after surveying
      Kafka/NATS/RabbitMQ/SQS/pgmq/pg-boss: `topic.Register(ctx, ds,
      topic.Config{Name, ...})` — `Register` because it matches
      `consumer.Register`, the codebase's existing idempotent
      startup-ceremony verb, while `Ensure*` stays reserved for silent
      partition self-heal — called at the composition root, safe to run
      on every boot. Missing → creates the catalog row + `message_log_<id>`
      + first partition; exists with matching config → no-op returning the
      existing topic; exists with different config → plain
      `ErrTopicConfigMismatch`. That last case is SQS's
      create-as-config-assertion, deliberately not RabbitMQ's
      channel-killing 406 and not NATS `CreateOrUpdateStream`'s
      declared-config-silently-wins — drift should be loud, and repairing it
      a separate deliberate act.
- [x] Registering a topic creates `message_log_<id>` with its own local
      `BIGSERIAL`-equivalent sequence and its own first partition — same
      shape `001_messages` gives `message_log` today, same
      `CREATE TABLE IF NOT EXISTS` + catch-the-duplicate-race pattern
      `EnsureNextPartition` already uses for concurrency safety.
- [x] **Settled (2026-07-08) — evolving `message_log_<id>`'s own columns
      (8c's `compaction_key`, Phase 12's `partition_key`, or any future
      envelope field) is not a `migrations/` concern.** A migration file is
      static, author-time DDL against a fixed set of table names; which
      `message_log_<id>` tables exist is runtime state (`SELECT id FROM
      topics`) that no `.sql` file can enumerate. This project's migrations
      have zero precedent for dynamic/looping SQL (no `DO $$` blocks
      anywhere), so the fix stays in Go, colocated with `CreateTopic`'s
      table template in `pkg/topic/datastore.go` — same file owns both "what
      does a topic's table look like" and "how do we bring an existing one
      up to date," so the two can't drift apart. Mechanically: loop
      `topics`, run one `ALTER TABLE message_log_%d ADD COLUMN IF NOT
      EXISTS ...` per row — idempotent and self-healing, the same discipline
      `EnsureNextPartition` already uses. The one thing that makes this
      tractable at all: `message_log_<id>` is declaratively partitioned, and
      Postgres cascades a parent's `ADD`/`DROP COLUMN` to every existing
      *and future* partition automatically — so the real fan-out is one
      statement per **topic**, not per partition. No generic "evolve all
      topics" mechanism gets built ahead of need — 8c and 12 each write
      their own small, purpose-named function
      (`AddCompactionKeyColumn`/`AddPartitionKeyColumn`) when they actually
      land, sharing a tiny `allTopicIds(ctx)` helper for the enumeration.
      Down/rollback isn't one file either — same treatment, a paired
      `Drop*Column` function, run once, by hand, when actually needed.
- [x] `cursors`, `deliveries`, and `leases` each gain a `topic_id` column,
      folded into their original migrations (`002_cursors`, `003_deliveries`,
      `004_leases`) per house style — each table's actual key changes
      differently, not uniformly:
      - `cursors`' PK today is the single column `consumer_group`
        (`002_cursors.up.sql`) — it must become the composite
        `(consumer_group, topic_id)`, since one group reading two topics
        needs two distinct cursor rows.
      - `deliveries`' PK today is already composite,
        `(consumer_group, message_id)` — becomes
        `(consumer_group, topic_id, message_id)`, since `message_id` is only
        unique within a topic once each has its own sequence.
      - `leases`' PK today is `(token, consumer_group)` — `token` (a random
        UUID) already disambiguates, so the PK itself doesn't need to
        change, but `low`/`high` are meaningless without knowing which
        topic's id sequence they're a range of, so `leases` still needs the
        `topic_id` column even though its key shape doesn't.
- [x] `bindings` gains a `topic_id` column too, folded into `005_bindings`.
      Its actual schema today has a surrogate `id BIGSERIAL PRIMARY KEY` and
      only an index on `consumer_group` (no compound key on
      `consumer_group`/`pattern` exists to begin with) — this phase just
      adds `topic_id` alongside `consumer_group`/`pattern` and widens that
      index to `(consumer_group, topic_id)`, since one topic's `routing_key`
      vocabulary has nothing to do with another's.
- [x] `WorkConsumer`/`WorkProducer` gain a `Topic` identity alongside the
      existing `Group` — their constructors accept the resolved topic
      `topic.Register` returns (id already looked up, cached, never
      re-resolved per message). Every dynamic-SQL call site that
      hardcodes `message_log`/`message_log_%d` today interpolates the
      resolved `topic_id` instead.
- [x] The janitor (`EnsureNextPartition`, `DropExpiredPartitions`,
      `SweepExpiredPartitions`, and 8c's compaction pass once it exists)
      all become topic-scoped, operating on `message_log_<topic_id>`'s own
      partitions. `cursorFloor` becomes `MIN(committed) FROM cursors WHERE
      topic_id = $1` — this is the actual fix for 8a's filed TODO, and the
      per-topic partition set is the fix for 8c's.
- [x] **Settled (2026-07-08) — the topic owns its log-shape config.** Today
      each consumer group's `WorkConsumerConfig` (`RetentionTTL`,
      `PartitionSize`, ...) is set per `WorkConsumer` instance, so two groups
      reading the same table could already, oddly, run their janitors with
      different settings against shared partitions — an existing quirk 8b
      inherits. The idempotent-declare UX resolves it: `topic.Config` at
      `Register` time is where log-shape knobs live (`RetentionTTL`,
      `PartitionSize`, plausibly `AllowDropPastCommitted` — properties of
      the log itself), persisted as columns on the `topics` row (folded into
      its migration per house style); janitors read them off the topic and
      those fields leave `WorkConsumerConfig`. Genuinely per-consumer
      runtime knobs (`JanitorPollRate`, `JanitorSweepBatchSize`, poll rates,
      batch limits) stay. This is the convergence the ecosystem keeps
      landing on — pg-boss v10 made `createQueue` mandatory and NATS takes a
      full `StreamConfig` at declare precisely because per-topic config
      needs a durable home.
- [x] **Open question, not settled — what happens to the original
      `message_log` and every existing lab/example built against it.**
      `examples/phase_1/{consumer,producer}/main.go` and every lab so far
      (`reclaimlab`, `exceptionlab`, `routinglab`, `partitionlab`,
      `dropfloorlab`, `sweeplab`) all construct their datastore against one
      fixed, topicless `message_log`. Once every table is `message_log_<id>`
      behind an explicit-registration requirement, none of them compile
      against reality unmodified. Options: migrate them all to register and
      target a "default" topic (real work, touches every existing lab, but
      keeps them runnable as regression checks for this phase's own labs);
      or accept it as a deliberate breaking change and note which labs stop
      working until touched. Not deciding this before starting risks
      discovering it mid-phase the way `message_log_0`'s hardcoded width
      blocked 8a's labs until it got its own `AskUserQuestion`.

**Lab:**
- [x] Register two topics, publish to both, confirm each gets its own
      physical table and its own dense id sequence — ids don't leak or
      interleave across topics.
- [x] Confirm a badly-lagging group on topic B does not block a drop or
      sweep on topic A — the exact cross-topic contamination 8a's TODO
      flagged, proven fixed live, not just asserted.
- [x] Confirm `routing_key`/`bindings` still behave exactly as Phase 7
      proved (retroactive binding application, CURSOR-path
      filter-but-still-advance, LIFECYCLE-path gate-row-creation) — now
      scoped within one topic's table, unchanged in behavior.
- [x] Confirm the re-scoping claim directly: two `routing_key` slices
      sharing *one* topic still share that topic's drop floor — a lagging
      group on one slice still blocks a drop that would otherwise free space
      for the other slice in the same topic.
- [x] Confirm publishing or claiming against an unregistered topic name
      fails clearly, rather than silently creating one.

**Explain it back:**
1. Why does each topic need its own dense id sequence rather than sharing
   the system-wide one? What specifically breaks if they share it?
Answer: Cursors and partitions. When many topics share a sequence id they each have a subset of the full sequence conflating what should be
topic concerns to cross cutting concerns. For example retention: a system-wide id forcing retention to also by system-wide because of how we drop partitions
by looking at the timestamp of the max(id) in a partition. that max(id) could come from any topic. Additionally if a lagging consumer exists and we have 'don't 
drop past floor' functionality enabled we are forced to wait on that lagging consumer for EVERYTHING which is scoped to the entire datasource instead of just a
topic due to min(id) of cursors being system wide.
2. Why do `cursors`/`deliveries`/`leases` need a `topic_id` added to their
   keys, when they didn't need one before this phase?
Answer: technically leases does not because they token is bound to whatever entity that claims it ie group/topic consumer. But in general it is to make these entities unambiguous. Cursors needs to know which message_log id sequence (ie topic) they are keeping track of. Deliveries needs to know the same because a message_id could be very different messages in different message_log tables.
3. Why is topic registration explicit, when partition creation
   (`EnsureNextPartition`) is allowed to self-heal silently?
Answer: topic registration creates durable lasting user concerns. It has to construct a table and manage configuration some of which is immutable. Making a topic creating explicit forces the user to take a second and think through what they want and lowers the chance of mistakes or mismanagement. Partitions are abstracted away
constructs that users in general don't need to be concerned about and thier naming are strictly computed values while topic names are user defined.
4. `routing_key`/`bindings` survive this phase with their matching logic
   completely unchanged — so what did splitting into per-topic tables
   actually fix, and what did it deliberately leave unfixed?
Answer: It fixed most of what was explained in Q1. However retention / partitions are no longer system-wide they are topic scoped which is better but still a constraint ie you could not have per consumer retention / partition configuration.

**Done when:** two topics are physically separate with independent
sequences/partitions (proven live), a lagging group on one topic is proven
not to affect another topic's drop/sweep, `routing_key`/`bindings` behave
identically to Phase 7 within a topic, `TODO.md`'s two filed pointers (8a's
global floor, 8c's fan-out cost) are closed out or updated to reflect the
fix, NOTES.md, `git tag phase-8b`.

**Real systems:** Kafka topics (the direct analog — a topic *is* its own
partition set, its own `retention.ms`, independent of every other topic);
"topic sprawl" as a known anti-pattern in real Kafka deployments is the
argument *against* going further and collapsing `routing_key` into topic
identity too.

---

### 8c — Log compaction: latest-per-key, filtered at claim time

**Concept:** Kafka's compacted topics keep only the latest event per key, but
still get there by appending a new record per write and deleting older
records for that key in the *background*, once a segment ages out — the log
is mutated after the fact, never in place. The tempting shortcut is to skip
the background step entirely: give the producer a `uniqueness_key` and have
it `UPSERT` — insert if new, update in place if the key already exists. That
breaks something the background-delete approach doesn't: a group that has
already committed past a key's original `id` never revisits it, so an
in-place update at that same `id` is invisible to that group *forever*, not
merely delayed — the opposite of what a compacted topic is supposed to
guarantee (every consumer eventually sees the latest value, however far
behind it starts). It also collides with 8a: id order is retention's
stand-in for time order, and an old `id` whose content keeps changing forever
undermines the "safe to drop, nothing here will change again" assumption
`partitionExpired` relies on.

So the log stays append-only, exactly as every earlier phase built it — a
write is always a new `id`, never a mutation. What changes is *where*
duplicates get resolved. Instead of a background janitor physically deleting
superseded rows once a watermark floor says it's safe (the approach
`reference/waterline/compaction.go` takes), this phase filters **at claim
time**: `readMessages`/`FanOut` only return the row that is *currently* the
latest for its key — older rows for that key still physically exist in
`message_log`, just never selected again once a newer one exists. This
removes the floor as a *correctness* requirement — nothing is ever deleted,
so nothing can ever be deleted too early. The floor doesn't disappear,
though: it downgrades from a correctness gate to an optional, whenever
-convenient disk-space cleanup, decoupled entirely from what a claim is
allowed to return.

"Currently the latest for its key" is itself an unbounded question — nothing
statically rules out a newer row existing anywhere ahead of it in the log —
so this phase resolves it two ways, deliberately, not by accident. The
*definition* is a correlated existence check ("nothing with a higher `id`
and the same key exists, anywhere"), evaluated directly against
`message_log`; that's ground truth, and cheap enough for a key whose own
writes cluster close together. But for an old, never-superseded key in a
long-lived topic, evaluating that definition directly means a scan to the
current tail, and the cost only grows as the topic keeps accumulating
history behind it. So the plan pairs the definition with a companion index
table, `latest_keys(topic_id, compaction_key, latest_id)`, upserted
synchronously in the same transaction as every keyed write — turning "what's
the latest for this key" from an O(partitions since the row) scan into an
O(1) lookup once it's in place. The correlated scan lands first (it's the
spec this phase has to satisfy regardless of how it's made fast, and its
cost is measured with real labs before the index is built, to size what the
index actually buys); `latest_keys` is the performance layer the design
always intended to carry the read path once that cost is confirmed to
matter, not a patch bolted on after a scaling surprise.

**Build:**
- [x] Each topic's own `message_log_<id>` gets `compaction_key TEXT` (landed
      in `createTopicLog`, not a `migrations/` file -- per-topic tables are
      created dynamically per Phase 8b). `NULL` means "not compacted," same
      convention as `routing_key`.
      **A partial index, `(compaction_key, id) WHERE compaction_key IS NOT
      NULL`, was added here to make the ground-truth scan's per-partition
      `newer` lookup cheap -- then dropped once `latest_keys` landed and
      the read path stopped querying `message_log` by `compaction_key`
      altogether.** Verified via `EXPLAIN`: `message_log`'s own primary key
      index is all the new lookup needs (it queries `latest_keys` by its
      own PK instead), so the partial index had zero live consumers left --
      confirmed by checking it doesn't appear in the new predicate's plan
      at all, not even as a rejected candidate. Its only remaining
      consumers were `compactionwidthlab`/`compactionscalelab`, which
      deliberately keep the OLD `NOT EXISTS` query alive as a frozen
      historical measurement -- dropping the index changes their absolute
      numbers (already recorded above) but not their pass/fail, since
      those assertions are about partition-touch-*count* relationships,
      not the access method. `compactionlab`'s own `EXPLAIN` check
      (`explainNoCompactionSubplan`) had silently drifted to testing this
      stale query shape after the read-path swap -- caught and fixed to
      test the real current predicate before deciding to drop the index.
- [x] Producer: `WorkProducer.Produce`/`AppendMessage` take a
      `ProduceOptions{RoutingKey, CompactionKey string}` struct (not two
      positional strings -- matches this codebase's existing `*Config`
      convention for optional per-call knobs, e.g. `topic.Config`). `""` →
      SQL `NULL`, same as `routing_key`.
- [x] **CURSOR and LIFECYCLE paths, decided: the predicate is unbounded, not
      bounded by the claim's own high.** Originally planned as "the max `id`
      for its `compaction_key` at or below the claim's own high bound" --
      revised after working through a concrete failure case. A claim's
      `high` is pinned once (stored on its `leases` row) and reused
      identically on every reclaim of that lease -- so a *bounded* check
      would need to look ahead, or risk this exact race: claim `(0,2]` reads
      row1 (`user:1`) before a competing write; the worker crashes before
      `Commit()`; a newer row for `user:1` is published; `ReclaimWithCursor`
      re-reads the identical `(0,2]` range and now excludes row1, since it's
      no longer the max within that fixed window. Row1 gets zero completed
      delivery attempts, not at-least-one -- looked like a real at-least-once
      violation.

      The resolution: at-least-once for a *compacted* key was never a
      per-message guarantee to begin with -- it's at-least-once delivery of
      that key's *current latest value*, exactly what Kafka's own compacted
      topics document (a caught-up consumer sees the final value; nothing is
      promised about every intermediate one). Row1 being superseded and
      dropped is correct, not a gap: row5 (the row that superseded it) still
      owes its own at-least-once delivery, and if row5 crashes and gets
      raced too, that obligation just carries forward again, converging once
      writes to the key stop outpacing successful delivery. So the predicate
      is simply "nothing with a higher `id` and the same `compaction_key`
      exists, anywhere" -- unbounded, re-checked live on every read,
      including reclaims. `readMessages` (CURSOR, shared by
      `ReclaimWithCursor` and `ClaimMessages`) and `FanOut` (LIFECYCLE) carry
      the identical predicate. `FanOut` was never bounded by a claim high to
      begin with (it already scans current state each call), so this is
      actually a better fit for it than a bounded version would have been --
      and it makes the two paths' predicates *exactly* identical, not just
      same-shaped.

      Note this predicate is NOT itself racy the way the original
      lease-reclaim concern was: `FanOut`'s decision to materialize a
      `deliveries` row is one-time and durable (once inserted it's retried
      via `ClaimMessagesWithLifecycle`'s own machinery, never re-decided),
      and `readMessages`' re-evaluation on reclaim is fine now that the
      guarantee it's held to is per-key, not per-message.
- [x] **Decided: no schema-level tombstone at all — pure application
      convention.** Considered relaxing `payload` to nullable (`payload IS
      NULL` = tombstone, matching Kafka and
      `reference/waterline/compaction.go` exactly) or adding a dedicated
      `is_tombstone BOOLEAN` column. Rejected both: Kafka's own reason for a
      *protocol-level* tombstone marker is so its background compactor —
      which physically deletes rows — can recognize a deletion generically
      across every topic without understanding that topic's payload schema,
      and eventually purge the tombstone itself after a retention window.
      This phase never physically deletes anything (that's the entire point
      of filtering at claim time instead of background-deleting), so that
      motivating reason doesn't apply here. The filter query already
      delivers "whatever the latest row for this key is" with zero
      special-casing regardless of what's inside it — a tombstone is not a
      distinct case at the log layer at all, just an ordinary row whose
      *payload* the application has chosen to give a deletion meaning to.
      So "how do I delete a key" is answered entirely by what the producer
      puts in `payload`, not by anything this framework provides:
      ```go
      // application-defined convention, not a framework field
      type UserProfile struct {
          UserID  string `json:"user_id"`
          Deleted bool   `json:"deleted"` // true = tombstone, by this app's own convention
          // ... other fields, zeroed/omitted when Deleted
      }

      // publishing a deletion is just publishing a normal message
      wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx) (*UserProfile, error) {
          return &UserProfile{UserID: "123", Deleted: true}, nil
      }, routingKey, "user:123" /* compactionKey */)

      // the consumer checks the convention it defined, not a log-level flag
      if profile.Deleted {
          cache.Delete(profile.UserID)
      } else {
          cache.Set(profile.UserID, profile)
      }
      ```
      A future generic disk-space cleanup pass (already noted below as
      optional/deferred, not part of this phase) would lose the ability to
      recognize "this key is fully dead, purge every row for it" without
      understanding each topic's schema — accepted as a real but currently
      unneeded cost, revisit if that cleanup pass ever gets built.
- [x] **Sizing the case for `latest_keys`.** 8b landed (each topic has its
      own id sequence and partition set), which already bounds the ground
      -truth scan's cost to one topic's own volume instead of the whole
      system's. Before spending a schema/write-path/read-path chunk sequence
      building the index, `compactionwidthlab` (`just compaction-width-lab`)
      measured what the scan costs *without* it, via
      `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF)`, counting partitions the
      correlated `newer` subplan's `Append` node *actually executed*
      against (distinct from partitions merely *listed* in the plan — Append
      always lists every child; Postgres tags a skipped one `(never
      executed)`, and only non-tagged lines count as a real touch):

      - **Narrow topic (`PartitionSize=4`, 40 rows, 11 partitions):**
        proving id 1 ("stale", never superseded) is the latest touched
        **all 11 partitions** — genuinely a full scan, not a missing
        optimization: with the row in the very first partition, every other
        partition's range could theoretically hold a newer id, so nothing is
        statically excludable. Proving id 39 ("fresh" v1, superseded by v2 at
        id 40, one partition over) touched **only 2 partitions** (9 and 10)
        — Postgres's runtime partition pruning skipped every partition
        strictly before the row's own, then the anti-join stopped as soon as
        partition 10 produced a match.
      - **Wide topic (`PartitionSize=50`, same 40 rows, 1 partition):** both
        cases touched **1 partition** — the tension disappears entirely once
        everything fits in one partition, not because the query changed.

      So the earlier worry was correctly shaped but the "proving a negative"
      case is worse in the worst case (a stale, near-the-start key never
      superseded: full scan to the tail) than "finding a match" typically is
      — but a match's actual cost isn't free either, it's bounded by how far
      the row's own partition sits from wherever (in partition order) the
      superseding write landed, which can be just as bad as the negative
      case if that write is old. In this project's own labs (small, bursty
      workloads, single-digit-millisecond `Execution Time` even at the
      11-partition full scan), the absolute cost was negligible — but the
      *shape* of the tradeoff (full-scan-to-tail is real, not a formality)
      is confirmed, not just theorized.

      **Follow-up, `compactionscalelab` (`just compaction-scale-lab`):**
      the width lab above only had 11 total partitions, so "full scan"
      looked small. This lab grows ONE topic's history in checkpoints (10 →
      50 → 200 → 500 → 1000 partitions, extending the same topic rather than
      reseeding) and re-measures the SAME never-superseded row's negative
      -proof cost fresh at each step — the actual shape of a backlog
      consumer replaying an old, never-superseded key on a topic that keeps
      growing underneath it:

      ```
      partitions   rows       touched    exec_ms
      10           99         10         0.120
      50           499        50         0.353
      200          1999       200        1.071
      500          4999       500        4.213
      1000         9999       1000       9.953
      ```

      Growth is linear, no amortization: roughly **10µs of fixed cost per
      partition**, and every single checkpoint re-touches every partition
      from the row's own through the current tail — nothing about proving
      this same row's status gets cheaper as history piles up behind it,
      it gets strictly more expensive, forever, for as long as that key is
      never superseded.

      **Confirms `latest_keys` is the right layer, not a coarser
      `PartitionSize`.** A coarser partition width helps the *current
      -snapshot* tension (11 partitions → 1 collapses both cases to the
      trivial floor) but doesn't bound the *lifetime* case: it sets the
      constant for a topic's total row count at a point in time, not the
      partition count a long-lived topic accumulates over its life. A topic
      that runs for a year has however many partitions a year of writes
      produced, regardless of how wide each one is; a backlog consumer
      replaying that topic from offset 0 pays the scan-to-current-tail cost
      for every never-superseded key it walks past, and the tail keeps
      moving further away as the topic keeps aging underneath the replay.
      Extrapolating the measured ~10µs/partition linear rate: a topic that's
      accumulated on the order of 100K partitions over its life would cost
      roughly **1 second per surviving key** touched during a full backlog
      replay — a replay touching thousands of such keys adds minutes of pure
      query overhead, independent of and on top of whatever the consumer's
      own processing does. `latest_keys` collapses this to a flat O(1)
      lookup regardless of partition count or topic age — the numbers above
      are why that index, not a wider partition, is the layer this design
      relies on for the lifetime case.

      **Side finding, unrelated to read cost but found while running this
      lab at scale:** `compaction-width-lab`'s cleanup (`topic.Destroy` on a
      2000-partition topic) failed with Postgres's own "out of shared
      memory" — dropping a partitioned parent needs an `ACCESS EXCLUSIVE`
      lock on every partition and every object each one owns (measured: 5
      lockable relations per partition — table, pkey index, partial
      `compaction_key` index, TOAST table, TOAST index), and Postgres's
      lock table is fixed-size at server start
      (`max_locks_per_transaction × (max_connections + max_prepared_transactions)`
      — 6400 on this dev Postgres's stock defaults). ~1000 partitions
      (~5000 locks) destroyed fine; ~2000 (~10000 locks) didn't. Not
      specific to compaction — any topic that accumulates enough partitions
      hits this on `Destroy`. Filed as its own `TODO.md` entry (batch the
      drop the way 8a's `dropPartition`/`sweepBatch` already do, instead of
      one giant transaction) rather than fixed here — out of this phase's
      scope. This, plus the read-cost finding above, also motivated a
      second `TODO.md` entry: a built-in "default alerts" concept for
      surfacing exactly this kind of silent-until-it-happens operational
      cliff before a user hits it blind.

      Alternatives considered and rejected in favor of the index:
      1. **A per-claim aggregate CTE** (`GROUP BY compaction_key` once, join
         the claim's candidate rows against it) instead of one correlated
         subquery per row — better for a claim touching many distinct keys
         at once, worse for small/frequent claims since it forces scanning
         the whole key space regardless of how few keys the claim actually
         touches. Workload-dependent, not a clear win, and doesn't change
         the underlying O(partitions) shape per key the way the index does.
      2. **Coarser `PartitionSize` for compacted topics alone** — helps the
         *current-snapshot* tension (confirmed: 11 partitions → 1 collapsed
         both cases to the trivial floor) but, per the numbers above, does
         NOT bound the *lifetime* backlog-replay case, since partition count
         keeps growing with a long-lived topic's total volume regardless of
         width. Still free and worth doing alongside the index, just not a
         substitute for it.

      An earlier pass at this design flagged the companion table as "Kafka's
      background-cleaner problem reintroduced in miniature" — that
      comparison doesn't hold: Kafka's compactor is asynchronous (an
      eventual-consistency window, its own crash-recovery story) and
      physically deletes data (reintroducing a correctness-critical floor).
      A synchronous, same-transaction UPSERT has neither property — it
      doesn't delete anything, and there's no window where it's stale
      relative to what's committed. That's what makes `latest_keys` the
      right shape for this design specifically, not Kafka's approach in a
      smaller costume.
- [x] **`latest_keys` — the O(1) index the read path resolves against.**

      - **Table shape, and a decision it needs:** `message_log` went
        per-topic-table in 8b because its row count scales with every
        message ever published. `latest_keys` scales with DISTINCT
        `compaction_key` count instead — usually orders of magnitude
        smaller — so the better default is a single SHARED table with a
        `topic_id` column, matching `cursors`/`deliveries`/`leases`/
        `bindings`'s post-8b shape, not `message_log`'s per-topic-table one:
        ```sql
        CREATE TABLE latest_keys (
            topic_id       BIGINT NOT NULL,
            compaction_key TEXT   NOT NULL,
            latest_id      BIGINT NOT NULL,
            PRIMARY KEY (topic_id, compaction_key)
        );
        ```
        This is a real architectural call the way 8b's chunk 2 `Topic`
        -as-field-vs-param question was — flag it for explicit review
        before building, don't just assume the shared-table default is
        right.
      - **Write path — same transaction, not a background job.** This is
        the property that makes it NOT a version of Kafka's background
        compactor: `pkg/producer/datastore/postgres.go`'s `appendMessage`
        gains a second statement in the SAME transaction as the
        `message_log` INSERT, only when `CompactionKey != ""` (mirrors the
        `NULLIF` convention — zero write-amplification for unkeyed traffic,
        the common case):
        ```sql
        INSERT INTO latest_keys (topic_id, compaction_key, latest_id)
        VALUES ($1, $2, $3)
        ON CONFLICT (topic_id, compaction_key) DO UPDATE
        SET latest_id = EXCLUDED.latest_id
        WHERE latest_keys.latest_id < EXCLUDED.latest_id;
        ```
        The `WHERE latest_id < EXCLUDED.latest_id` guard is load-bearing:
        `BIGSERIAL` allocates an id at `INSERT` time, not commit time, so
        two concurrent publishes to the SAME key can commit out of id
        order under READ COMMITTED. The guard compares by id VALUE, not
        commit order, so it converges to the true max regardless of which
        transaction's UPSERT lands first — same discipline as this phase's
        crash/reclaim reasoning: don't trust arrival order, trust the id.
        Same-transaction atomicity also means there's never a window where
        `message_log` has a row `latest_keys` doesn't know about yet — no
        eventual-consistency gap the read path has to account for.
      - **Read path.** `readMessages` and `FanOut` swap the unbounded `NOT
        EXISTS` scan for a direct lookup:
        ```sql
        m.compaction_key IS NULL
        OR m.id = (SELECT latest_id FROM latest_keys
                   WHERE topic_id = $N AND compaction_key = m.compaction_key)
        ```
        O(1) instead of O(partitions since the row) — the whole point.
        `latest_keys` is authoritative for every keyed row from the moment
        the write path lands: no backfill, no fallback path for a key with
        no `latest_keys` row. This project has no compatibility requirement
        to conserve — there is no live deployment with compacted-topic
        history predating this table, so a migration mechanism for one
        would be designing for a user that doesn't exist. The old unbounded
        `NOT EXISTS` predicate is deleted outright once this lookup lands.
      - **Known tradeoff, not a blocker — now measured, not just reasoned
        about.** The `ON CONFLICT DO UPDATE` takes a row lock, so publishes
        to the SAME hot key now serialize on that key's `latest_keys` row
        (they didn't before — plain `message_log` appends never contended).
        `latestkeyswritelab` (`just latest-keys-write-lab`) quantifies it:
        sequential, uncontended publishes showed no measurable fixed cost
        (unkeyed vs. keyed differed within noise, both ±10-20%, no
        consistent direction — the extra statement itself is cheap). 50
        concurrent goroutines each publishing to their OWN key vs. all 50
        hammering ONE key showed the real cost: 2.5-2.9x slower under full
        serialization, reproduced across repeated runs. A follow-on burst
        of ~1,000 same-key updates left ~500 dead tuples on `latest_keys`,
        pending autovacuum — real but bounded bloat, not unbounded growth,
        since the table only ever holds one row per (topic, key) regardless
        of how many times that key is republished. A non-issue for
        many-distinct-keys workloads (this phase's whole design target);
        worth knowing if a workload ever concentrates writes on one
        screamingly hot key.
      - **Chunk shape** (own plan file, `~/.claude/plans/` — same convention
        as every other phase's chunk breakdown): (1) schema + migration, (2)
        write-path UPSERT, (3) retention/janitor cleanup in `dropPartition`/
        `sweepBatch` (below), (4) read-path swap in `readMessages`/`FanOut`
        — deletes the old unbounded predicate outright, (5) lab proving
        correctness under concurrent same-key races (the id-guard
        invariant) and re-running `compactionscalelab`'s growth curve
        against the NEW read path to confirm it stays flat regardless of
        partition count.
- [x] **Decided: 8a's retention needs no compaction-awareness — dropping or
      sweeping a compacted key's last surviving row is intentional
      expiration, not a bug.** Raised as an open question: what happens
      when `DropExpiredPartitions`/`SweepExpiredPartitions` removes the
      partition (or row) holding the CURRENT latest row for some
      `compaction_key`? Both functions judge purely by age
      (`partitionExpired`'s partition-level `created_at` check,
      `sweepBatch`'s per-row `created_at < cutoff` check) with zero
      awareness of `compaction_key`/`latest_keys` today.

      The resolution follows from the mental model anyone combining
      `RetentionTTL` with compaction on the same topic already has:
      "retention" already means "a key that hasn't been touched in longer
      than the TTL window ages out" in every TTL'd key-value system (Kafka's
      own `cleanup.policy=compact,delete`, DynamoDB TTL, an expiring cache).
      Nobody expects "retention" to mean "keep this forever no matter what,
      except delete literally everything else" just because a message
      happened to carry a `compaction_key`.

      This composition already falls out correctly from an invariant this
      design already relies on elsewhere — id order tracks time order, and
      compaction always keeps the highest-id row for a key. As long as a
      key keeps getting written to inside the TTL window, its current
      -latest row keeps landing in a fresh, young partition, immune to both
      gates. The ONLY way a key's sole surviving row ends up old enough to
      expire is if that key genuinely hasn't been written to in longer than
      `RetentionTTL` — at which point letting it go is retention doing
      exactly what it was configured to do. A row that's merely superseded
      (not a key's current latest) was already always safe to drop/sweep
      regardless of age, no interaction with retention at all.

      So `dropPartition`/`sweepBatch` need no new gate — adding one (make a
      compacted key's latest row immune to retention entirely) was
      considered and rejected: it would make `RetentionTTL` mean two
      different things depending on whether a message happens to carry a
      `compaction_key`, which is the actually-surprising behavior, not the
      thing to guard against.

      **What DOES need handling: `latest_keys` cleanup, not prevention.**
      When a key expires this way, its `latest_keys` row becomes a
      permanent ghost — pointing at an `id` that no longer exists, with
      nothing left to ever supersede it. Both janitor paths already have
      the exact precedent for this (each already cleans up orphaned
      `deliveries` rows in the same transaction as its own delete) — extend
      the same pattern to `latest_keys`:
      - `dropPartition` (range-based, matching its existing `deliveries`
        cleanup's shape): `DELETE FROM latest_keys WHERE topic_id = $1 AND
        latest_id >= $2 AND latest_id < $3` (the partition's `[low, high)`).
      - `sweepBatch` (exact-id-based, matching its existing `deliveries`
        cleanup's shape): `DELETE FROM latest_keys WHERE topic_id = $1 AND
        latest_id = ANY($2)` — reusing the same `ids` slice already
        collected via `sweepBatch`'s own `RETURNING id`.

      Both are safe and precise without any extra correctness check:
      `latest_keys.latest_id` only ever equals a key's CURRENT latest id
      (by construction of the write-path UPSERT's guard), so these deletes
      only ever match when the row being reaped genuinely was that key's
      last surviving version — a superseded row's id was never pointed to
      by `latest_keys` in the first place, so sweeping/dropping it can
      never accidentally orphan a still-live key. An earlier idea to
      instead *alert* on this (tying into the "default alerts" TODO
      concept) was considered and dropped — alerting on retention doing
      exactly what it was configured to do would be noise, not signal;
      that concept is for genuinely unexpected operational cliffs, not
      routine expiration.

**Lab:**
- [x] Publish several versions of the same `compaction_key`; confirm a claim
      spanning all of them returns only the latest, and that the older rows
      still physically exist in `message_log` (filtered, not deleted).
      Verified in `compactionlab` (`just compaction-lab`): 6 published rows,
      claim returns `[3,4,6]`, physical row count stays 6.
- [x] Confirm a message superseded *after* it was already delivered isn't
      retroactively unsent — a version already delivered stays delivered,
      compaction only ever affects what a not-yet-resolved read returns.
      Verified: `user:3` v1 delivered+committed, v2 published after, v1
      stays physically present and `committed` never regresses.
- [x] Confirm the crash/reclaim race directly: claim a range containing a
      keyed row, let its lease expire without committing, publish a newer
      version of that same key, then reclaim the same range and confirm the
      superseded row is now correctly excluded — proving the predicate is
      unbounded (re-checked live on reclaim) rather than pinned to what
      existed at original claim time, and that this is safe because the
      newer row still owes its own delivery. Verified: `WORKER 1` crashes
      holding `user:4` v1, `WORKER 2`'s reclaim of the identical range
      returns zero messages once v2 supersedes it, v2 still delivers on its
      own claim afterward.
- [x] Confirm a tombstoned key's latest row still gets delivered, not
      silently dropped — the consumer decides what a tombstone means, not
      the query. Verified on both CURSOR and LIFECYCLE paths with a
      `Deleted: true` payload.
- [x] `EXPLAIN` a claim over unkeyed traffic and confirm the compaction
      predicate's subquery never executes (the `OR` short-circuits on
      `compaction_key IS NULL`) — watch the plan, don't assume it. Verified
      via `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF)`: plan shows `SubPlan
      ... on message_log_<id> newer (never executed)`.
- [x] **Partition-width tension with 8a, measured.** `compactionwidthlab`
      (`just compaction-width-lab`) ran the identical 40-row, same-key-shape
      workload against `PartitionSize=4` (11 partitions) and `PartitionSize=50`
      (1 partition). Narrow: proving a never-superseded row is latest touched
      all 11 partitions, proving a just-superseded one touched 2. Wide: both
      cases touched the single partition. 8a wants many small partitions for
      drop granularity; this phase's ground-truth scan wants few large ones
      for cheap resolution — confirmed as a real, measured tension, not a
      guess, with a coarser `PartitionSize` as the direct lever for the
      current-snapshot case (`latest_keys` is the lever for the lifetime
      case, since it removes the scan — and the tension with it — entirely).
- [x] **Does the planner actually optimize the lookup?** Verified: no `Merge
      Append` (there's no ordering requirement for an existence check, so
      Postgres plans a plain `Append` under a `Nested Loop Anti Join`), but it
      does NOT scan every live partition regardless — runtime partition
      pruning skips everything strictly before the row's own partition, and
      the anti-join stops as soon as any partition in scan order produces a
      match (proven via `(never executed)` tags on the skipped children in
      `EXPLAIN ANALYZE`'s output). Proving a negative gets no such benefit:
      with nothing to match, every unprunable partition genuinely executes.
- [x] **`latest_keys` correctness under concurrent same-key races.**
      `latestkeysracelab` (`just latest-keys-race-lab`) fired 50 goroutines
      publishing to the SAME `compaction_key` at once; `latest_keys`
      converged to the true `MAX(id)` afterward, proving the `WHERE
      latest_id < EXCLUDED.latest_id` guard live, not just read off the
      SQL. Same lab re-ran `compactionscalelab`'s exact checkpoints (10 →
      1000 partitions) against the new lookup: touched message_log
      partitions stayed at 1 and execution time stayed flat
      (~0.015-0.023ms) at every checkpoint, where the old `NOT EXISTS` scan
      grew linearly with history size — because the new lookup never scans
      message_log's history at all, it's a single PK lookup on
      `latest_keys` plus the row's own id.
- [x] **Retention expiring a compacted key's last surviving row.**
      `latestkeysretentionlab` (`just latest-keys-retention-lab`) covers
      both janitor paths: `dropPartition` rolling a dormant key's sole
      partition out of active use, and `sweepBatch` reaping an
      individually-expired row from a partition that never rolls. Both
      confirm the row AND its `latest_keys` entry are gone afterward (no
      ghost row) while a key touched inside the TTL window survives —
      `sweepBatch`'s case specifically re-touches and re-sweeps 3 times to
      confirm it isn't a first-pass fluke.

**Explain it back:**
1. Why doesn't this design need a watermark-safe floor to gate correctness,
   unlike Kafka's own compacted topics (and this repo's
   `reference/waterline/compaction.go`)? What does the floor become instead?
Answer: Because correctness is garenteed at produce / write time it is not an async process that needs an additional correctness gate due to potential lag.
The floor for us is just the standard cursor committed value (not claimed -- claimed can regress on a crash/reclaim, committed is the crash-safe frontier),
and it's no longer a correctness gate -- it downgrades to an optional, whenever-convenient disk-space cleanup, decoupled from what a claim can return.
2. Why can a brand-new consumer group get latest-per-key on its very first
   claim under this design, when a background-delete design can't give it
   that for free?
Answer: Because 'latest' is garenteed after producer transaction is complete. So the claim query will always get lastest id for compaction_key
A background delete design has some amount of lag before compaction is complete and as such has not strong garentee you will get latest, it is dependent on
size of background-delete lag
3. Why does the filter search unboundedly for a key's latest write instead
   of pinning to the claim's own high (`id <= $hi`)?
Answer: Because the gaurentee we hold by for a compacted topic is not 'at-least-once per message' it is 'at-least-once per latest compacted_key'.
A bounded check would only be wrong on reclaim specifically: a lease's high is pinned once and reused on every retry, so after a crash a newer write
landing outside that fixed window would be invisible to a bounded check -- the reclaimed row would look 'locally latest' within the stale window even
though it's actually been superseded. Unbounded means the check re-evaluates live against current state every time, not the state pinned at claim time.
4. Why is the `compaction_key` index partial (`WHERE compaction_key IS NOT
   NULL`) instead of covering every row?
Answer: Because compaction_key is not the standard consumer setup and we would incur write overhead for no reason.
(Note: this index was dropped entirely later in 8c once latest_keys made it a dead consumer -- this answers why it WAS partial, not what exists today.)
5. Phase 8b split every topic into its own physical table and its own dense
   id sequence. Why does that help *this* phase's compaction lookup
   specifically — what did a shared, system-wide `BIGSERIAL` cost a single
   topic's own key-latest search before 8b existed?
Answer: It doesn't matter for latest_keys itself -- that lookup is O(1) regardless of partition count or sequence density by construction. It still matters
for the scan though, which is still the spec this phase's read path has to satisfy underneath latest_keys: before 8b, proving a negative meant scanning
across every OTHER topic's interleaved traffic sharing the same BIGSERIAL, not just this topic's own volume. 8b bounds that scan to one topic's own
history -- it just doesn't buy the index anything, since the index sidesteps the scan entirely.

**Done when:** a claim over keyed traffic returns exactly one row per key
(proven live, not assumed), unkeyed traffic shows zero added query cost via
`EXPLAIN`, the tombstone convention is decided and documented, `latest_keys`
lands and the read path resolves against it in O(1) regardless of partition
count, retention (8a) correctly expires a genuinely-dormant compacted key
with no `latest_keys` ghost rows left behind, NOTES.md, `git tag phase-8c`.

**Real systems:** Kafka compacted topics (same goal, different mechanism —
Kafka's own compaction is still background/segment-based, not read-time
filtering); Cassandra/DynamoDB-style last-write-wins resolved at read time is
the closer analog to what this phase actually builds.

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
- [x] **Datastore-blip retry backoff.** New `pkg/retry` package: `Retry`
      (generic, explicit-wrap `RetryableError`/`PermanentError` classification,
      context-aware exponential backoff, no wasted sleep after the last
      attempt) and `DatastoreRetry` (embeds `Retry`, auto-classifies raw pgx
      errors via `pgClassify` — default-deny: only `pgconn.SafeToRetry`/
      `pgconn.Timeout`/`net.Error` are retryable, everything else (a
      `*pgconn.PgError`, context cancellation, an unrecognized app sentinel)
      is permanent by default, which is what keeps `pkg/retry` free of any
      import on `pkg/consumer`). Every `Datastore[Message]` interface method
      `pkg/consumer`/`pkg/producer`/`pkg/topic` actually call got a
      public/private split — the public method wraps a call to the identically
      named private one in `DBRetry.Wrap`, so call sites (`consumer.go`,
      `Janitor`'s five goroutines included) read exactly as before, no retry
      plumbing visible at the call site.

      This surfaced a real correctness gap beyond "just retry": `AppendMessage`
      re-runs the caller's `producerFunc` on retry, and if a commit's ack is
      lost after it actually landed (the classic ambiguous-commit problem),
      blindly retrying would double-publish. Fixed with an idempotency key —
      `ProducerFunc` takes one, `ProduceOptions.IdempotencyKey` lets a caller
      supply their own (cross-restart protection) or leave it unset (a fresh
      `uuid.NewV7()` per call, same-call-only protection) — claimed against a
      new `idempotency_keys` table (`ON CONFLICT DO NOTHING`, checked *before*
      the `message_log` insert) the same way Kafka's idempotent producer uses
      a broker-assigned PID + monotonic per-partition sequence number to let
      the broker silently no-op a duplicate resend. Can't be a unique
      constraint on `message_log` itself (Postgres requires a partitioned
      table's unique constraints to include the partition key) — same reason
      `latest_keys` exists as its own table for compaction. Swept by the
      janitor on `Topic.IdempotencyKeyTTL` (DB-persisted, defaults to 24h,
      *not* "0 = keep forever" like `RetentionTTL` — an unbounded
      `idempotency_keys` table is a bug, not a policy anyone should opt into).

      Unlike `latest_keys` (opt-in per keyed message), the claim gate was
      paid on *every* publish — measured (`idempotency-keys-growth-lab`) at
      20-36%+ extra disk vs. `message_log` itself, and (a throwaway
      benchmark, not a lab) 15-30% lower sustained throughput and 56-70%
      higher CPU per message on the DB server, from the second network round
      trip and the second index-maintaining write. Two fixes:
      - **Batching:** the claim INSERT and the `message_log` INSERT collapsed
        into one round trip via a data-modifying CTE
        (`WITH claim AS (... RETURNING 1) INSERT ... WHERE EXISTS (SELECT 1
        FROM claim) RETURNING id`) — the log insert only fires if the claim
        actually landed a row, same "already committed" semantics as before,
        `pgx.ErrNoRows` replacing the old `RowsAffected() == 0` check. Cut
        the throughput hit to ~0-19% and the CPU-per-message hit to ~19-30%.
      - **`ProduceOptions.SkipIdempotency`:** a per-call opt-out (default
        `false`) — mirrors Kafka's own move from opt-in to default-on
        idempotence (3.0+) rather than the reverse, since the caller can't
        control whether `DBRetry` itself retries an ambiguously-committed
        attempt. Per-call, not per-topic, since `IdempotencyKey` was already
        per-call and mixed traffic (critical + high-volume telemetry) is
        realistic within one topic.

      `SkipIdempotency` forced a deeper fix: `DatastoreRetry`'s blanket
      "retry anything blip-shaped" can't stay safe once retry-safety depends
      on caller context. Only `Commit()` is genuinely ambiguous (every
      earlier statement fails inside an uncommitted, atomically-rolled-back
      transaction, so retrying those is always safe); retrying an ambiguous
      `Commit()` is only safe when `idempotency_keys` can catch the
      duplicate. First cut: `producerDatastore` took the base `retry.Retry`
      instead of `DatastoreRetry` and classified every error by hand
      (`classifyTransient` pre-`Commit`, `classifyCommit(err,
      skipIdempotency)` at `Commit`) — correct, but it meant every non-`Commit`
      call site had to remember to wrap its own error, and `consumer`/`topic`
      were assumed to have "no equivalent ambiguity" so they kept the old
      blanket `DatastoreRetry` untouched.

      That assumption turned out wrong: reviewing the graceful-shutdown work
      below surfaced that `consumer`'s `commit`/`partialCommit` have the exact
      same ambiguous-`Commit` shape, just without an `idempotency_keys`-style
      guard to make a retry safe — a retried `Commit` that already landed
      re-`INSERT`s the same parked `deliveries` row and hits its `PRIMARY KEY`
      (a real, uncaught error, not the survivable false `ErrLeaseLost` its own
      lease `DELETE` produces on retry). Fixed by unifying the mechanism
      instead of special-casing consumer too: `DatastoreRetry`'s default
      `Wrap` classification *is* what `classifyTransient` used to be (retry
      anything blip-shaped) — except it now passes an already-classified
      `RetryableError`/`PermanentError` through untouched instead of
      re-deciding it. That pass-through is what lets one shared `Wrap`
      coexist with a package classifying its own `tx.Commit()` inline, right
      at the call site (an `if`/`else` returning `retry.NewPermanentError`/
      `retry.NewRetryableError`/a plain `err`) — no standalone
      `classifyCommit` helper needed, and every other error return goes back
      to being a plain `return err`.

      `consumer` classifies inline in both `commit` and `partialCommit`, but
      *asymmetrically* — a distinction that only fell out once the two were
      compared side by side. `commit`'s lease `DELETE` is self-consuming: a
      retry after an already-landed-but-unacked commit finds the lease row
      gone and bails via `ErrLeaseLost` *before ever reaching the `parkSql`
      inserts* — so `commit` is unconditionally safe to retry, no guard
      needed. `partialCommit`'s lease `UPDATE` (narrowing `low`) is *not*
      self-consuming — the row survives, so a retry's `UPDATE` matches it
      again and reaches `parkSql` a second time. That's the one branch that
      actually needs the `hasParkedRows` check (never retry when there's
      something to park, since `deliveries` has no `ON CONFLICT DO NOTHING`
      the way `idempotency_keys` does). Considered and declined: adding `ON
      CONFLICT DO NOTHING` to `partialCommit`'s `parkSql` insert instead,
      which would let it drop the guard entirely — safe today (ranges never
      overlap, so the only way to hit that `PRIMARY KEY` is a retry of this
      exact call), but that safety leans on an invariant enforced elsewhere
      in the file rather than being locally provable the way `commit`'s
      `DELETE`-based guard is; a future regression in range-disjointness
      would silently no-op instead of crashing loudly. `topic` needed no
      inline classification at all — both its transactions (`upsertTopic`,
      `deleteTopic`) are `ON CONFLICT DO NOTHING`/`IF EXISTS`/no-match-safe-
      `DELETE` throughout, so re-running either one whole after an ambiguous
      commit was already safe, and stays safe under the shared default.
      `pkg/retry` still exports `IsTransientPgError` as the one "does this
      look like a blip" primitive every inline classification and the
      default classifier both build on.
- [x] **Graceful-shutdown lease truncation.** `CursorClaim`'s per-message loop
      now checks `ctx.Done()` between messages (not mid-message — a hard
      per-message timeout is the separate, still-open item below): each
      iteration selects on `ctx.Done()` before attempting the next message,
      so a shutdown signal stops the batch from taking on new work without
      needing `context.WithTimeout`'s cooperative-cancellation help mid-call.

      "Update its low bound" wasn't an existing capability — `Commit` frees a
      lease atomically with no partial/narrowing mode — so a new datastore
      method, `PartialCommit`, does the actual work: same `deliveries`-parking
      shape as `commit`, but instead of `DELETE FROM leases`, an `UPDATE
      leases SET low = $lastProcessed` narrows the *same* lease (same token,
      `until`, `reclaims`) rather than freeing it. The untouched suffix
      `(lastProcessed, high]` stays leased under that worker's now-dead token
      until it naturally expires and reclaims normally — no new expiry
      mechanism, it rides the existing crash-recovery path, just over a
      shorter range than the original claim.

      The commit call itself needed its own context: the `ctx` that triggered
      the interruption is already `Done`, so writing `PartialCommit` through
      it would fail immediately. Same fix as `Shutdown` already uses for
      `ShutdownFunc` — `context.WithTimeout(context.WithoutCancel(ctx), ...)`
      — reusing `AckMargin` as the bound, since that's already the config
      knob for "extra time to record outcomes after processing." An
      interruption before any message resolves (`lastProcessed` still at the
      lease's original `low`, no exceptions/terminals) skips the call
      entirely rather than issuing a no-op `UPDATE`.

      One correctness subtlety caught before it became a bug: the interrupted
      branch can't be detected by comparing `lastProcessed` against the
      lease's `high` — `claimed.Messages` is already filtered by routing/
      compaction, so a fully-successful, *uninterrupted* range routinely ends
      with `lastProcessed < high` whenever the tail of the raw id range
      contains non-matching ids. Branching on that would misclassify an
      ordinary complete run as an interruption. Fixed with an explicit
      `interrupted` bool set only inside the `ctx.Done()` case, independent
      of where `lastProcessed` lands.
- [x] **`recover()` around the `consumerFunc` call.** A bare Go panic (nil map
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
- [x] **Hard per-message timeout via a detached-goroutine race.** `WorkTimeout`
      is validated today but never enforced as a deadline — it only feeds the
      lease-duration math (an *earlier* version of this code did wrap the call in
      `context.WithTimeout`, still visible commented-out around `consumer.go`'s
      dead V1/V2 paths, but that wrapping was dropped and never replaced). A bare
      `context.WithTimeout` isn't enough on its own anyway: it's cooperative
      (just closes `ctx.Done()`), so it does nothing for a call that never checks
      its context (blocking cgo, a tight CPU loop, a library that ignores ctx).
      Built into `callSafely` — the same shared helper the `recover()` bullet
      above added, all three call sites unchanged — rather than a separate
      wrapper: `consumerFunc` now runs in its own goroutine, racing a buffered
      `done` channel against a timer so the *caller* can give up independent of
      the callee. Go has no goroutine kill: this converts "one message hangs the
      whole range, worker gets externally killed, whole range reclaimed and
      re-crashes" into "one message leaks one goroutine" — better containment,
      not a full fix.

      Moving `consumerFunc` into its own goroutine broke the `recover()`
      bullet's own guarantee in a way that only showed up on review:
      `recover()` only catches a panic on the same goroutine's call stack, so
      the panic-recovery `defer` has to live *inside* the spawned goroutine, not
      in `callSafely`'s own frame. An earlier draft had it in the wrong place —
      assigning straight into `callSafely`'s named return from the child
      goroutine — which was both a data race (an unsynchronized write racing
      the parent's own `return`) and functionally silent: `done` never received
      anything on a panic, so `callSafely` waited out the *entire* `WorkTimeout
      + grace` before returning, and the real panic message was discarded in
      favor of the generic timeout error. Fixed by having the goroutine's own
      `recover()` send into `done` like any other result. Verified with
      `-race`: a throwaway test panicking `consumerFunc` returned in ~120µs
      with the panic message and stack trace intact, no race detected.

      The race needs a grace period past `ctx`'s own deadline — without one,
      `ctx.Done()` firing and the hard timeout firing at the same instant would
      make the `select` a coin-flip that can discard even a `consumerFunc` that
      respected cancellation and returned in time. Exposed as
      `WorkTimeoutGrace` (config, not hardcoded), since the right size is a
      property of the *caller's* `consumerFunc` — `pkg/consumer` can't know how
      long someone else's cleanup takes. Measured the one part it *can* know:
      Go's own scheduler wakeup latency from a context deadline firing to a
      blocked goroutine sending on a channel, at p99 < 1ms even under
      artificial GC/scheduler pressure (2000 trials, occasional OS-level jitter
      outliers into the tens of ms). That means almost none of the default
      100ms default is scheduling risk — it's discretionary slack for the
      caller's own cancellation-response time (e.g. a `pgx` cancel-request
      round trip), sized to roughly one same-region network round trip since
      that's this project's own deployment shape. Folded into the
      `ShutdownTimeout` validation inequality alongside `WorkTimeout`/
      `AckMargin`.

      Timeout errors report `messageID`, never `work` itself — `last_error` is
      DB-persisted (`deliveries.last_error`), so formatting the raw payload
      into it (`%v`/`%+v` on a generic `*WorkType`) would leak message contents
      into a column with far wider read access than an in-process error ever
      had.
- [x] **Track abandoned goroutines.** Once calls can be abandoned, a small
      in-process registry — keyed per **(message, attempt)**, not per-message,
      since the same message can time out on more than one attempt and a
      per-message key would let the second overwrite the first's entry. Expose
      a counter (`hard_timeouts_total` — how often this happens), a gauge
      (`abandoned_outstanding` — how many are leaked *right now*, the direct
      leak-prediction signal), and a histogram (late-finish latency, for the
      ones that do eventually return). Tag `last_error` distinctly for a
      hard-timeout abort (e.g. `"hard timeout after Ns, goroutine abandoned"`)
      so it's queryable without extra infra.

      Split from Phase 10's "Operational metrics" bullet on purpose: the
      registry (the `(message, attempt)`-keyed map + raw counters) is built
      here, as plain in-process state — just enough for this phase's own lab
      to read directly and assert against (that lab bullet already assumes a
      gauge exists: "the abandoned goroutine shows up in the gauge"). Treat
      counter/gauge/histogram as informal values for now, not a commitment to
      any metrics library. Phase 10 wires these already-built numbers into
      whatever pluggable logger/metrics interface it settles on, instead of
      designing the tracking logic from scratch — avoids inventing a one-off
      metrics shape now that gets redone once Phase 10 lands.

**Lab:**
- [x] `just idempotency-keys-lab` — mechanism-level proof for the retry
      backoff item above, not harness-level fault injection (that's the next
      bullet, still open): a retried `AppendMessage` under the same key lands
      exactly once even though `producerFunc` re-runs every time (the
      documented contract); distinct keys never collide; an unset key
      protects only within one call, not across separate publishes; the
      sweep drains expired claims in bounded batches while a live one
      survives; `IdempotencyKeyTTL` round-trips through a topic
      re-registration without falsely tripping `ErrTopicConfigMismatch` (the
      exact bug an earlier, unpersisted version of this field had);
      `SkipIdempotency=true` writes zero claim rows and genuinely
      double-publishes under a repeated key (the honest cost, not hidden),
      while a protected call right after in the same topic is unaffected.
- [x] `just idempotency-keys-growth-lab` — the storage/throughput axis, not
      mechanism correctness: `idempotency_keys` size vs. `message_log` size
      at increasing checkpoints with no sweep running (the 20-36%+ overhead
      number above), and a sustained-load scenario running the real janitor
      sweep cadence concurrently, confirming steady-state size stays bounded
      near Little's Law's `rate * ttl` instead of growing toward the full
      published count, and drains to zero once ttl passes.
- [x] `just delete-topic-lab` — not a Phase 9 mechanism, but this phase's own
      `idempotency_keys` was the last of six tables `DeleteTopic` needed to
      cascade-clean (cursors/leases/deliveries/bindings/latest_keys joined
      it). Seeds one row in all six via the real datastore methods,
      deliberately leaving a lease OPEN and a deliveries row unclaimed (the
      messiest mid-flight state, not the already-resolved case), and
      confirms `Destroy` drains every one of them plus `message_log` itself.
- [x] `just fault-isolation-lab` — induces all three failure modes through the
      real `CursorClaim` path (not the interactive `--fail-rate`/`--crash-after`
      harness — a dedicated deterministic lab, same convention as every other
      Phase 6.5c+ lab) plus a direct check of `pkg/retry`: a panicking
      `consumerFunc` and one that hangs past `WorkTimeout+WorkTimeoutGrace`
      each isolate to the one message in a 3-message batch (the other two
      process normally, the range still fully commits), the hang's
      abandoned-goroutine gauge shows exactly 1 outstanding immediately after
      `CursorClaim` returns and self-clears once that detached goroutine
      actually finishes sleeping, and a forced transient failure that clears
      within `MaxRetries` is fully invisible to the caller.

      That last check caught a real, previously-undiscovered bug in
      `pkg/retry.Retry.Wrap`, unrelated to anything built this phase:
      `IsRetryable(nil)` correctly returns `false` for a successful call, but
      the code treated "not retryable" as synonymous with "permanent failure,
      return the joined error" — so a call that succeeded on, say, its 3rd
      attempt still returned `errors.Join(err1, err2, nil)`, which
      `errors.Join` collapses to a **non-nil** error (it only discards nils,
      not the whole result) wrapping the two already-resolved prior failures.
      Every caller's `if err != nil { return err }` (literally every call
      site in `producer`/`consumer`/`topic`) would treat a successful
      retry-then-recover as a hard failure — for `CursorClaim` specifically,
      that would have propagated up through `Process`'s own `if err :=
      p.CursorClaim(...); err != nil { return err }` and killed the whole
      polling loop, the exact opposite of "a DB blip doesn't kill the
      consumer." Verified with a standalone repro before and after: before
      the fix, 2 injected failures + a 3rd successful call returned a
      non-nil error; after adding an explicit `if err == nil { return nil }`
      short-circuit ahead of the retryable/permanent classification (which is
      meaningless for a nil result anyway), all four cases (retry-then-
      succeed, immediate success, exhausted retries, immediate permanent
      failure) behave correctly. Confirmed no regression by re-running every
      existing lab that exercises `DatastoreRetry` end-to-end.
- [x] `just shutdown-truncation-lab` — drives `CursorClaim` directly (not
      `Consume`) so cancellation timing is deterministic: a `consumerFunc`
      closure cancels the shared context after message 2 of 3. Confirms
      message 3 is never attempted; the lease survives narrowed to `(2,3]`
      with one parked exception at message 2; `AdvanceWaterline` stays
      correctly pinned at message 1 by the *unresolved exception* even though
      the lease is already narrowed past it (the two blockers combine via
      `LEAST`, neither short-circuits the other); once the exception resolves
      the waterline jumps straight to the narrowed low without waiting on the
      untouched suffix's lease; and once that (now-shorter) lease naturally
      expires, reclaiming it returns exactly the one untouched message, never
      re-delivering the already-resolved prefix.

**Explain it back:**
1. Why does a recovered panic have to go through the *exact same*
   retry/backoff/dead path as an ordinary error, instead of its own
   special-cased handling?
Answer: recovered panics are not necessarily permanent errors (nil map write, index out of range, bad type assertion). The fact is we don't know if a 
retry will help or not b/c we don't know the consumerFuncs code. So it is better to go on side of caution and follow standard expected path instead
of making assumptions
2. Why is `context.WithTimeout` alone insufficient to enforce `WorkTimeout`,
   and what does the detached-goroutine race actually buy you given Go has no
   goroutine kill?
Answer: context timeouts expect to be explicitily handled. Normally via a call to ctx.Err or ctx.Done. Our own internal code we can do that for. However
we cannot gauruntee that the user does that within their consumerFunc. Because of that we have a detached-goroutine race that allows us to internally exit
the consumerFunc work such that we may retry or mark dead within the users expected WorkTimeout + Grace period. The one caveat this brings is that the goroutine
that was raced is still running and as such we have a abanonded routine which we track via metrics
3. Why key the abandoned-goroutine registry by (message, attempt) rather than
   by message alone?
Answer: If first and second attempt of a message was abandoned. The second attempt would overwrite the first within registry despite their potentially being two
real live abandoned go routines. message & attempt is the uniquness identifier for the goroutine and as such should be the key

**Done when:** all three induced failure modes recover correctly and are
demonstrated in the lab, NOTES.md, `git tag phase-9`.

**Real systems:** this is the same isolation problem every worker framework
solves — Sidekiq's `retry` + `death_handlers`, Temporal's activity heartbeats
(the model for `9b`'s deferred lease-heartbeat item), Kubernetes liveness probes as
the outer backstop for faults nothing in-process can catch.

---

## Phase 10 — Observability: logging & the rollup model

**Concept:** right now the only way to see consumer health is to query
Postgres directly. This phase adds the operator-facing surface — a pluggable
logger, a metrics snapshot any Prometheus- or OTel-compatible backend can
consume with zero vendor code living in this repo, and a live debug readout
built on the same data — and settles the lazy-vs-synchronous waterline
question this plan deliberately deferred back in 6.5b ("make this a lazy roller
off the hot path — staleness only delays GC"). No `reference/waterline`
counterpart either: it has no logger and no debug surface of its own (it's a
teaching artifact meant to be read, not operated), so there's nothing to
compare against — you're building past what the reference bothered with.

**Build:**
- [x] **Common logger interface.** The internal `pkg` logging should accept a
      logger *interface*, not a hardcoded implementation, so callers can plug
      in their own structured or unstructured logger.
- [x] **Writer-based default logger.** Provide a default implementation that
      takes an arbitrary `io.Writer`, so a caller with no opinions gets
      something reasonable for free.
- [x] **Queue-state query.** A datastore method that computes, live, per
      `(group, topic)`: backlog/lag (`head − committed`, the waterline gap),
      the `claimed − committed` inflight gap (how many messages/batches are
      currently outstanding vs. resolved — the metric gap the lazy-rollup
      question originally flagged), `ready`/`inflight`/`dead` exception
      counts (retry depth and DLQ size), oldest-unacked age, and open-lease
      count. These numbers are DB-truth, not in-process state — multiple
      consumer processes share one `cursors`/`deliveries` state, so nothing
      short of a live query can answer "what's true right now." This is the
      direct generalization of the `just lag` Justfile recipe, which already
      does this ad hoc for one topic. Everything below is built on top of
      this one query — nothing else in this phase invents a second way to
      compute these numbers.
- [x] **Metrics snapshot.** Merge the query above with the in-process
      counters Phase 9's "Track abandoned goroutines" bullet already built
      (`hard_timeouts_total`, `abandoned_outstanding`, reclaim latency —
      still living on `ConsumerMetrics`, deliberately left as informal values
      until this phase) into one struct/method: a single call returning the
      full current picture, DB-truth and in-process numbers together. The
      debug readout and the OTel instruments below both read from this one
      snapshot, not from the query or `ConsumerMetrics` separately.
- [x] **OpenTelemetry metrics integration.** Accept a `metric.Meter`
      (`go.opentelemetry.io/otel/metric` — the API package only, never the
      SDK or a specific exporter) as a config option, defaulting to the
      global no-op provider so a caller who supplies nothing pays zero cost.
      Register one instrument per snapshot number: `ObservableGauge` for the
      DB-truth numbers (the callback re-runs the query above), `Counter`/
      `UpDownCounter` for the in-process ones. This is the interoperability
      layer: the moment a caller wires up the OTel Prometheus exporter or a
      Datadog OTLP exporter in their *own* app, every instrument registered
      here shows up there — with zero Prometheus- or Datadog-specific code
      ever living in this repo. (Precedent: River's `rivercontrib/otelriver`
      — API-only in the core module, the actual vendor wiring lives in a
      separate, opt-in package.)
- [x] **Debug/metrics readout.** A `String()`/print method that formats the
      snapshot for a human on demand — the free, zero-dependency consumer of
      the exact same data the OTel instruments expose to machines. This is
      the "query Postgres directly" replacement the phase's concept note
      promised, scoped per `(group, topic)` since a group can read more than
      one topic.
- [x] **Resolve the lazy-vs-synchronous rollup.** Decide, with measured
      numbers, whether `AdvanceWaterline` should stay a periodic lazy tick or
      become synchronous — advance right after a lease/batch resolves instead
      of waiting for the next tick. Record the latency-vs-overhead tradeoff you
      measured, not just the decision. This isn't only an observability nicety:
      `committed` is exactly what 8a's `cursorFloor` reads (`MIN(committed)
      FROM cursors`) to decide whether a partition is safe to drop — a
      synchronous rollup makes retention itself more responsive, a concrete,
      already-shipped stake beyond "the debug numbers look fresher."

      **Decided: stay lazy.** New permanent lab, `examples/phase_1/rolluplab`
      (`just rollup-lab`), drives the real `ClaimMessagesWithCursor`/
      `Commit`/`AdvanceWaterline` datastore methods directly across three
      scenarios, numbers below representative of several runs:

      | Metric | Lazy (150ms poll) | Synchronous |
      |---|---|---|
      | Staleness — avg | ~77ms | ~2ms |
      | Staleness — max | ~145ms | ~3–11ms |
      | Fixed cost, uncontended | ~1.9ms/op (baseline) | ~2.7ms/op (+30–50%) |
      | Concurrent (20 workers, same cursor row) | ~1.3–1.5ms/op (baseline) | ~1.7–2.8ms/op (1.3x–1.9x slower) |

      Staleness scales linearly with poll interval — at the real default
      `ClaimPollRate` (5s) that's avg ~2.5s / max ~5s under the lazy roller,
      not the 150ms-poll numbers above (150ms was chosen only to keep the lab
      fast; the mechanism under test doesn't depend on the specific interval).
      The synchronous option collapses that to ~2ms, but `Commit` today
      touches only `leases` (DELETE by token) and `deliveries` (INSERT) — it
      never touches `cursors`, so concurrent workers holding separate leases
      on the same `(group, topic)` commit in full parallel right now. A
      synchronous `AdvanceWaterline` adds an `UPDATE cursors` to that path,
      which *is* new lock contention on a row every concurrent committer in
      the group shares — measured at 1.3x–1.9x slower with 20 concurrent
      committers, a cost that doesn't exist at all in the current design.

      That's a bad trade: retention drop decisions and a debug readout
      tolerate multi-second staleness fine, but a synchronous rollup taxes
      every single commit's latency, permanently, for every consumer group,
      to buy a sub-5-second wait down to a few milliseconds. Kept
      `RollWaterline` lazy and added `WorkConsumerConfig.WaterlinePollRate`
      (0 defaults to `ClaimPollRate`, same pattern as `JanitorPollRate`) so
      staleness is tunable independently of the commit hot path when a
      shorter wait is actually needed, without introducing contention.

**Lab:**
- [x] Use the harness (`--fail-rate`, `--sleep`, `--crash-after`) to induce
      every failure mode you've built and watch the metrics snapshot react.
      If a failure doesn't move a number, you have a blind spot.

      New permanent lab, `examples/phase_1/metricsreactionlab`
      (`just metrics-reaction-lab`): drives each harness failure mode through
      the real `WorkConsumer`/`Datastore` paths and diffs the metrics
      snapshot before/after, asserting each induced failure moves EXACTLY
      the number(s) it should and nothing else — the executable form of this
      bullet's own check. Four scenarios in sequence: a retryable failure
      (`ready` exception), sustained failure exhausting retries
      (`ready`→`inflight`→`dead`), a hard timeout/hang (abandoned goroutine,
      then a `ready` exception, then self-clear), and a crash mid-range —
      claimed but never committed (an orphaned lease, then reclaimed once it
      expires). All six tracked numbers (`ReadyExceptions`,
      `InflightExceptions`, `DeadExceptions`, `OpenLeases`,
      `AbandonedRoutines.Outstanding`/`.Total`) move at least once, each
      attributable to a distinct trigger — no blind spots.
- [x] Run the consumer under load with the debug readout on; watch the
      `claimed`/`committed` gap and exception counts move in real time as you
      inject failures with the existing harness. Confirm lowering
      `WaterlinePollRate` (added by the lazy-vs-synchronous decision above)
      visibly narrows how fast `committed` catches up, without needing the
      synchronous path.

      New permanent lab, `examples/phase_1/metricsloadlab`
      (`just metrics-load-lab`): runs the real `Consume` loop (not direct
      datastore calls — the live, end-to-end counterpart to rollup-lab's
      measurement) under a pre-seeded burst, once at a slow `WaterlinePollRate`
      (2s) and once fast (100ms), printing the debug readout as `committed`
      catches up to `head`. Measured a 15.6x cut in catch-up time (2.03s →
      130ms) in one representative run — consistent with rollup-lab's own
      staleness-scales-with-poll-interval finding, now proven through the
      real ticker-driven goroutines. A second scenario injects retryable
      failures into a live run: the readout shows `ready` exceptions climb
      (0→10→3→0) and `committed` close the gap on `head`, fully draining with
      zero dead-letters and no manual intervention — `DrainExceptions`
      retrying live through the same loop.
- [x] Point a real OTel Prometheus (or OTLP) exporter at the `metric.Meter`
      you pass in and confirm every instrument shows up on the other end —
      proof the integration works end-to-end, not just that it compiles
      against the API.

      New permanent lab, `examples/phase_1/otelexportlab`
      (`just otel-export-lab`): the only place in the repo that imports
      `otel/sdk` or a specific exporter — wires a real
      `sdkmetric.MeterProvider` backed by `otel/exporters/prometheus`'s
      Reader (its own registry, not the global `DefaultRegisterer`), drives
      real consumer activity (a retryable failure and a hard-timeout
      abandon+self-clear, so the synchronous `AbandonedRoutines` instruments
      have an actual data point, not just the always-live `QueueState`
      gauges), then scrapes over a real `httptest` HTTP server via
      `promhttp.HandlerFor` — exactly how Prometheus itself would collect it.
      All 13 instruments (10 `QueueState` gauges + 3 `AbandonedRoutines`)
      confirmed present in the scraped body. First run caught a real gap in
      the lab itself, not the product: `AbandonedRoutines`' synchronous
      instruments (`Counter`/`UpDownCounter`/`Histogram`) only emit a data
      point once actually recorded to — unlike `QueueState`'s
      `ObservableGauge`s, which report every collection via their callback
      regardless — so a run that never abandons a goroutine correctly never
      shows them on a scrape. Fixed by driving a real abandon+self-clear
      before scraping, not a product bug.

**Explain it back:**
1. What's the tradeoff between a lazy periodic rollup and a synchronous one —
   what do you gain and what do you pay for each?
2. Why does a live debug readout of claimed/committed/exception-count matter
   even though the underlying data was always queryable in Postgres directly?
3. For each number in the metrics snapshot: which failure mode is it the
   early warning for?
4. Why does the OTel integration depend on `go.opentelemetry.io/otel/metric`
   (the API package) but never the SDK or a specific exporter like
   Prometheus's or Datadog's client?

**Done when:** the pluggable logger, the queue-state query, the metrics
snapshot, the OTel instrument integration, and the debug readout all work
end-to-end; the lazy-vs-synchronous rollup decision is made and recorded with
measured reasoning; NOTES.md, `git tag phase-10`.

**Real systems:** Kafka consumer-group lag exporters; the `slog` interface
pattern (Go's own answer to "pluggable logger"); Temporal/Sidekiq dashboards
built on exactly these counters (backlog, in-flight, dead-letter,
oldest-unacked); River's `rivercontrib/otelriver` package is the direct
precedent for this phase's OTel split — API-only in the core module, the
actual Prometheus/Datadog wiring lives in a separate, opt-in package.

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
against (`datastore.go` — 16 methods as of 8a, since it supports **both**
`CURSOR` and `LIFECYCLE` modes behind one interface, which the reference
deliberately keeps separate — see the "honest delta" note near the top of
this document). The count grew from 13 to 16 with 8a's
`EnsureNextPartition`/`DropExpiredPartitions`/`SweepExpiredPartitions` — as
good a concrete example as any for the audit below, since "partition" and
"TTL" are about as Postgres-specific as a method signature gets. One
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
      can publish to multiple queues/topics within the same transaction —
      today's `WorkProducer.Produce(ctx, producerFunc, routingKey)` only
      targets the one queue its `Datastore` was constructed against (the
      caller's `producerFunc(ctx, tx)` closure runs inside a transaction
      `Produce` opens and owns, but only one `AppendMessage` call is
      possible per invocation). Note this gets materially harder, not just
      bigger, once 8b lands: "multiple targets" today means multiple
      `routing_key`s in one shared table, but post-8b it means multiple
      physically separate `message_log_<topic_id>` tables, each with its own
      sequence — likely multiple `WorkProducer` instances needing to share
      one externally-managed transaction, inverting who currently owns it.
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
- [ ] Add `partition_key text` to `message_log` (nullable = no ordering) —
      not `events`, that's `reference/waterline`'s table name, not this
      project's (folds into `001_messages` per house style; if 8b has landed
      by the time this is built, it's per-topic on each `message_log_<id>`
      instead). Worth naming plainly why this is a *second* key column
      alongside 8c's `compaction_key` rather than reusing it: they answer
      different questions at different times — `compaction_key` is a
      read-time "what's current" filter, `partition_key` is a claim-time
      "don't run two of these at once" gate — a message could reasonably
      want one, the other, both, or neither.
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

## 8d — Latency: LISTEN/NOTIFY (deferred, optional)

*Deprioritized for now — cut from Phase 8 to keep that phase's own scope to
what's already shipped (retention, per-topic tables, compaction). Revisit
only if poll-interval latency is actually a measured problem, not a
theoretical one.*

**Concept:** the consumer's poll loop bounds message-pickup latency to the
poll interval. `LISTEN/NOTIFY` lets producers wake idle workers immediately
instead of waiting for the next tick — but `NOTIFY` is fire-and-forget: a
notification sent while no one is listening (or during a reconnect) is lost
forever, so a fallback poll has to stay in place underneath it.

**Build:**
- [ ] Add `LISTEN/NOTIFY` so producers wake idle workers instead of relying
      on poll interval, with a fallback poll for missed notifies and delayed
      (`run_at`) messages. Knowing *why* you keep the fallback poll (NOTIFY
      is fire-and-forget, lost if no listener) is the lesson.

**Explain it back:**
1. Why must `LISTEN/NOTIFY` keep the fallback poll? Name both message classes
   it would otherwise lose.

**Done when:** lab passes, NOTES.md, `git tag phase-8d`.

**Real systems:** Postgres `LISTEN`/`NOTIFY` itself; this is the same
at-least-one-fallback-poller pattern most Postgres-backed queues (River,
Oban) use underneath their own notify-based wakeups.

---

## 9b — Lease heartbeat/renewal (deferred, optional)

*Deprioritized for now — cut from Phase 9 to keep that phase's build limited
to correctness backstops that apply to every `consumerFunc` regardless of
workload shape (a panic or a hang breaks any range, no matter how long or
short the job). Heartbeat only matters for one specific shape: a job whose
*legitimate* runtime exceeds `WorkTimeout`. No workload in this project
needs that yet, and building it now means landing a new datastore method
(the renew `UPDATE`) right before Phase 11 redraws the consumer/datastore
boundary — real risk of redoing it. Revisit after Phase 11, once that
boundary has settled and Phase 10's debug readout (open-lease count,
per-group lag) exists to actually observe heartbeat behavior instead of
validating it blind. Pick this up only if a real long-running workload shows
up that needs fast crash-reclaim without a huge fixed `WorkTimeout`.*

**Concept:** for long-running jobs whose runtime legitimately exceeds
`WorkTimeout` but still want fast reclaim on a real crash: hand
`consumerFunc` a `heartbeat()`/`touch()` handle; the lease only extends when
touched (`UPDATE ... SET lease_until = now()+ext WHERE id=$1 AND
lease_token=$2`). `RowsAffected==0` on a renew means the lease was already
reclaimed — cancel the work context. Keep a hard max-duration ceiling
regardless, so a buggy progress loop that keeps touching forever still
eventually caps out. Opt-in; short jobs ignore it and rely on the fixed
lease window.

**Explain it back:**
1. Without heartbeat, how does a long-running job avoid getting reclaimed
   mid-flight by another worker? What has to change about `WorkTimeout` to
   make that work, and what does that cost every *other*, short-running job?
2. Why does a missed heartbeat renew (`RowsAffected==0`) mean "cancel the
   work context" rather than "retry the renew"?

**Done when:** lab passes, NOTES.md, `git tag phase-9b`.

**Real systems:** Temporal's activity heartbeats are the direct model here —
an activity worker calls `RecordHeartbeat` periodically, and the server
times out the activity if heartbeats stop arriving, independent of the
activity's own configured timeout.

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
  cleanup). Phase 12 (FIFO partitions), 6.5d (lane sharding), 7b
  (header/content routing), 8d (`LISTEN/NOTIFY` latency), and 9b (lease
  heartbeat/renewal) are optional and deliberately deferred to the end of
  this document — pick them up only if a real workload demands ordering,
  lane-level throughput, attribute-based routing a `routing_key` pattern
  can't express, poll-interval latency actually matters, or (9b,
  specifically after Phase 11 lands) a long-running job needs fast
  crash-reclaim without a huge fixed `WorkTimeout`.
- **The meta-lesson:** by Phase 6.5 you'll understand *in your hands* why Kafka,
  RabbitMQ, and Pulsar are different — they're the same primitives with different
  foundational defaults. That understanding is worth more than the code.
