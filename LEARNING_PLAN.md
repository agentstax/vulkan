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

## Status board

Update this as you go. One line per phase; the current phase gets the detail.

| Phase | State | Notes |
|---|---|---|
| 0 — Setup | ✅ done | golang-migrate + justfile + docker-compose wired |
| 1 — Durable atom | 🔨 **in progress** | remaining: delete-after-process, poll loop, graceful shutdown, two-worker lab |
| 1.5 — Transactional enqueue | ⬜ | |
| 2 — Lifecycle | ⬜ | |
| 3 — Competing consumers | ⬜ | ⚠️ batching already leaked into Phase 1 code — see trap T1 |
| 4 — Log/queue split | ⬜ | |
| 5 — Fan-out | ⬜ | |
| 6 — Synthesis | ⬜ | |
| 7 — Routing | ⬜ | |
| 8 — FIFO partitions | ⬜ | |
| 9 — Operational layer | ⬜ | |

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
every later phase, and in Phase 9 they're how you exercise the metrics.

---

## Phase 0 — Setup & the destination ✅

**Goal:** know where you're headed so each phase has a place to fit.

- [x] Stack: Go + Postgres (docker-compose locally, Railway Postgres available).
- [x] Migration tool: `golang-migrate`, driven by `justfile` recipes — you'll
      evolve the schema ~8 times, so migrations are part of the discipline.
- [x] Internalize the end-state **two-table split** you'll arrive at gradually:
  - `events` — immutable, append-only **log** (retention, replay, routing,
    partitions live here)
  - `deliveries` — mutable, per-(consumer, message) **lifecycle** state
    (ack/retry/dead-letter live here)
- [x] Don't build the split yet. Start with one table and split it at Phase 4 —
      feeling *why* the split is necessary is the lesson.

**Mental model to hold:** a **log** is "messages are facts you retain and re-read
at a cursor"; a **queue** is "messages are work you claim and consume." Every
system below is one, the other, or a deliberate fusion. You're building the
fusion.

---

## Phase 1 — The durable atom: append + atomic claim 🔨

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
- [ ] **Two workers, no collisions:** `just produce 20`, run two consumers with
      `--sleep 1s`, watch the printed messages interleave with no duplicates.
- [ ] **The SKIP LOCKED contrast:** remove `SKIP LOCKED`, rerun. Watch worker 2
      block behind worker 1 and the workers serialize. Put it back. That
      contrast is the whole lesson of this phase.
- [ ] **Kill mid-process:** run a consumer with `--sleep 5s`, `kill -9` it
      during the sleep, run `just peek` — the row is still there (tx rolled
      back, lock released). Start another consumer — it picks the row up.
- [ ] **Crash-after:** same proof via `--crash-after`, no manual kill needed.

**The aha:** `SKIP LOCKED` is what lets two workers run the exact same query at
the same instant and get *different* rows instead of one blocking the other.
And a crashed worker needs zero recovery code — the transaction rollback *is*
the recovery.

**Explain it back** (from memory, no peeking):
1. Why does the `DELETE` have to be in the same transaction as the claim? Walk
   through what can go wrong with each of the two orderings if it's separate.
Answer: 
2. A worker is killed with `kill -9` mid-process. Step by step, what does
   Postgres do, and when does the row become claimable again?
3. What does `SKIP LOCKED` change about the query's *result set*, exactly? Why
   is that safe here when skipping rows would normally be a correctness bug?

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
- [ ] Add a toy business table (e.g. `users`) via a new migration.
- [ ] An API shape question worth sweating: the producer currently owns its own
      pool and transaction. To enqueue inside the *caller's* transaction, the
      producer needs to accept a `pgx.Tx`. Design that (e.g.
      `Produce(ctx, tx, work)` or a `ProducerInTx` variant) — this exact API
      tension is why River's docs talk about "insert-only clients."
- [ ] Wrap a business write + a `jobs` INSERT in a single `BEGIN ... COMMIT`.

**Lab:**
- [ ] Force an error (rollback) *after* the business write but *before* commit;
      `just peek` both tables — confirm **neither** row exists.
- [ ] Commit successfully; confirm **both** exist and a worker picks up the job.
- [ ] **Visibility proof:** inside an open (uncommitted) producing transaction,
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
- [ ] Migration: add `status text` (`ready|processing|done|dead`),
      `attempts int default 0`, `run_at timestamptz default now()`,
      `locked_at timestamptz`, `last_error text`.
- [ ] Claim changes to `WHERE status='ready' AND run_at <= now()`, and instead
      of deleting: `UPDATE status='processing', locked_at=now(), attempts=attempts+1`.
      **Note the structural change:** claim and process are now *separate
      transactions*. The claim tx commits fast; processing happens unlocked.
- [ ] On success → `status='done'` (keep the row for now, so you can see history).
- [ ] On failure → if `attempts < max`: `status='ready', run_at = now() + backoff(attempts)`
      (exponential backoff); else `status='dead'`.
- [ ] **Lease / visibility timeout:** a reaper job that runs periodically:
      `UPDATE ... SET status='ready' WHERE status='processing' AND locked_at < now() - interval 'X'`.
      Recovers messages from workers that crashed mid-process.

**⚠️ Trap T2 — the zombie worker:** after the lease expires and the reaper
re-readies a message, the *original* worker may still be alive and slow — now
two workers are processing the same message. Your consumerFunc just became
at-least-once. You don't have to solve it in this phase (idempotency is the
real answer), but induce it in the lab and name it in NOTES.md.

**Lab:**
- [ ] `--fail-rate 0.3` with `max=3`: watch attempts climb, `run_at` push into
      the future (backoff), and stubborn messages land in `dead`.
- [ ] `SELECT * FROM jobs WHERE status='dead'` — that's your dead-letter queue,
      as a query. You can *see* every message's state, attempt count, and error.
- [ ] `--crash-after` a worker mid-process; watch the reaper return its job
      after the lease expires and another worker complete it.
- [ ] **Induce T2:** set `--sleep` longer than the lease. Watch the same
      message processed twice. Feel it.

**The aha:** the `FOR UPDATE` lock only lasts the *claiming* transaction — once
you commit the `processing` update, the lock is gone. So the durable "I'm
working on this" lease is the `status`+`locked_at` *data*, not the DB lock.
Getting this distinction is the single most important insight in the whole plan.

**Explain it back:**
1. In Phase 1, what held the claim? In Phase 2, what holds it? Why did it have
   to change? (Hint: what would a 10-minute job do to a Phase 1 transaction?)
2. Walk the full state machine including every transition's trigger.
3. Why does the reaper make delivery at-least-once rather than exactly-once?
   What property must the consumerFunc now have?

**Done when:** labs pass (including T2 induced and understood), NOTES.md,
`git tag phase-2`.

**Real systems:** RabbitMQ `nack`/`reject` + Dead-Letter Exchanges; SQS
visibility timeout + redrive-to-DLQ; Pulsar negative-ack + `maxRedeliverCount` →
DLQ; JetStream `maxDeliver` + `term`. The `run_at`+backoff is SQS/JetStream
delayed redelivery.

---

## Phase 3 — Competing consumers & batching

**Concept:** scale throughput by adding workers without double-processing, and
amortize round-trips.

**Build:**
- [ ] Run N worker goroutines (reuse your existing `pkg/concurrency` pool),
      each looping: claim → process → ack.
- [ ] Reintroduce batch-claim — `LIMIT 50 FOR UPDATE SKIP LOCKED` — but now
      answer the Trap T1 question deliberately: with Phase 2's state machine,
      claim is its own fast transaction and each message's success/failure is
      recorded *individually*. One bad message no longer poisons the batch.
      Write down in NOTES.md why the batch failure-domain problem from Phase 1
      dissolved.
- [ ] Add the **critical index**: a partial index
      `CREATE INDEX ON jobs (run_at) WHERE status='ready'`. Without it, the claim
      query table-scans as `done`/`dead` rows accumulate.

**Lab:**
- [ ] **Measure the index:** seed a few hundred thousand rows (mostly
      `done`/`dead`), `EXPLAIN ANALYZE` the claim query with and without the
      partial index. Record both numbers in NOTES.md. The index is the
      difference between a queue that stays fast and one that rots.
- [ ] **Find the ceiling:** plot throughput vs worker count (rough numbers are
      fine — msgs/sec at 1, 2, 4, 8, 16 workers). Find where it stops scaling.
      Knowing where Postgres-as-a-queue tops out (tens of thousands/sec) tells
      you when you'd ever need Kafka. Record the ceiling and your guess at the
      bottleneck.

**Explain it back:**
1. Why is the partial index so much better than a full index on `(status, run_at)`
   for this workload?
2. Batch claiming in Phase 1 had a failure-domain problem. Why doesn't Phase 3's
   batching have it?
3. What was your measured ceiling, and what do you think the bottleneck was —
   lock contention, WAL, round-trips, or the worker code itself? How would you
   tell?

**Done when:** both measurements recorded with numbers, NOTES.md,
`git tag phase-3`. **Phases 1–3 are a production-grade job queue** — this is
literally what River/Oban/graphile-worker are. Pause here and skim River's
docs; you'll recognize everything.

**Real systems:** Kafka consumer group (one partition → one consumer); RabbitMQ
multiple consumers on a queue. Batching = Kafka `max.poll.records`, SQS batch
receive.

---

## Phase 4 — The log/queue split: retention + replay

**Concept:** stop deleting. Separate the immutable record of what happened from
the mutable record of who's processed it. This is the Kafka model, and the
foundation for everything after.

**Build (the big refactor):**
- [ ] `events("offset" bigserial pk, topic text, payload jsonb, created_at)` —
      append-only, **never deleted on consume**. `offset` is the position.
      (Quoting note: `offset` is a reserved word in SQL — quote it or name the
      column `position`/`log_offset`.)
- [ ] `consumers(name text pk, position bigint)` — one cursor per consumer.
- [ ] A consumer reads
      `SELECT * FROM events WHERE "offset" > $position ORDER BY "offset" LIMIT N`,
      processes, then `UPDATE consumers SET position = $last`.
- [ ] **Replay** = `UPDATE consumers SET position = 0` (or to a timestamp's
      offset). Re-reads history.

**Lab:**
- [ ] Point a brand-new consumer at offset 0 and watch it replay the entire
      history independently of other consumers. That's the superpower Kafka has
      and RabbitMQ structurally cannot.
- [ ] `git diff phase-3..HEAD` — read your own refactor. Which code got
      *simpler* (no status machine on the hot path) and what capability got
      *lost*? That diff is the queue↔log tradeoff, in your own code.

**The aha:** the cursor is a **high-water mark** — a single integer. Note what
you just *lost*: you can no longer say "message 5 failed but 6,7,8 are done."
That hole is the exact tension you'll resolve in Phase 6. Feel the loss now.

**Explain it back:**
1. What exactly can a cursor not express that per-row status could? Give the
   concrete failure scenario.
2. Why does replay cost nothing extra in this design? What Phase 1 decision
   would have made it impossible?
3. When the consumer crashes *after* processing but *before* the cursor
   update, what happens on restart? What delivery guarantee does that imply?

**Done when:** labs pass, NOTES.md, `git tag phase-4`.

**Real systems:** Kafka (log + committed offsets in `__consumer_offsets`);
Pulsar (managed ledgers + per-subscription cursors). Retention-by-time is Kafka
`retention.ms`.

---

## Phase 5 — Fan-out to independent consumers

**Concept:** many consumers, each with their own cursor over the same log, each
at its own pace.

**Build:**
- [ ] You already have `consumers.position` keyed by name — so multiple named
      consumers reading the same `events` is *already* fan-out. Formalize it: a
      `consumer_group` concept where each group has an independent position.
- [ ] Compute **lag**: `(SELECT max("offset") FROM events) - position` per
      group. This is your health metric.

**Lab:**
- [ ] Add a new group while the system runs; it starts at the earliest retained
      offset and catches up without affecting the others.
- [ ] Slow consumer A to a crawl (`--sleep`); consumer B stays current. Watch
      their lags diverge. Independent consumption confirmed.

**The aha:** fan-out is free *because* you retained the log and made the cursor
per-consumer. Deleting on consume (Phase 1) made this impossible; retaining
(Phase 4) made it trivial. One design decision, two chapters apart, unlocks it.

**Explain it back:**
1. Why is fan-out structurally impossible in the Phase 1–3 design?
2. What's the operational risk of a consumer group that's permanently slow,
   once retention (Phase 9) exists? (This is Kafka's "consumer fell off the
   retention window" failure.)

**Done when:** labs pass, NOTES.md, `git tag phase-5`.

**Real systems:** Kafka consumer **groups**; Pulsar **subscriptions** (each
subscription is an independent cursor); JetStream **durable consumers**.

---

## Phase 6 — Reconcile lifecycle + fan-out (the synthesis)

**Concept:** give *each* consumer group per-message lifecycle (Phase 2) over the
*shared* log (Phase 4). The hard, interesting part — and where Pulsar earns its
keep.

**Build:**
- [ ] New table:
      `deliveries(consumer_group text, event_offset bigint, status, attempts, locked_at, last_error, PRIMARY KEY(consumer_group, event_offset))`.
- [ ] A "fan-out" step (a projector per group, or a lazy insert) materializes a
      `deliveries` row per (group, event) as events arrive.
- [ ] Each group now claims *its own* delivery rows with `SKIP LOCKED` + the
      Phase 2 state machine. The `events` log stays immutable; lifecycle lives
      entirely in `deliveries`.
- [ ] Keep a cheap path: broadcast/replay consumers that don't need lifecycle
      keep using a bare cursor (Phase 5). Only lifecycle-needing groups get
      delivery rows. Per stream, choose cursor vs delivery-rows.

**Lab — the synthesis demo (the one that proves you understand the whole
problem space):**
- [ ] Group A dead-letters message 5 (per-group DLQ via
      `WHERE consumer_group='A' AND status='dead'`) while group B processes
      message 5 fine and group C is replaying from offset 0 — all on the same
      log, simultaneously. Script this as a `just` recipe; it's your demo.

**The aha:** you can have retention/replay/fan-out **and** per-message
ack/retry/DLQ simultaneously — by separating the immutable log from mutable
per-consumer delivery state. Also feel the cost: delivery rows are N× writes for
N groups (write amplification). That cost is *why* Kafka punts lifecycle to
"retry topics" instead.

**Explain it back:**
1. Draw the end-state architecture from memory: tables, who writes what, who
   reads what. (This was Phase 0's "destination" — you've now arrived.)
2. Quantify the write amplification: 1000 events, 5 lifecycle groups, 2
   cursor groups — how many rows written?
3. For a given new stream, how do you decide cursor vs delivery-rows? Name the
   deciding question.

**Done when:** synthesis demo runs from one recipe, NOTES.md,
`git tag phase-6`. **You've graduated from "queue" to "log+queue platform."**
Phases 7–9 are polish — stop here if it's enough.

**Real systems:** Pulsar **Shared** subscriptions with individual acks +
per-subscription DLQ (the canonical "lifecycle on a log"); JetStream
`AckExplicit`. Kafka's non-answer: separate retry/DLQ topics.

---

## Phase 7 — Routing

**Concept:** producers don't address consumers; they publish with attributes, and
bindings decide who receives.

**Build:**
- [ ] `events` already has `topic`; add `routing_key text` and use the
      `headers`/`payload` JSONB.
- [ ] `bindings(consumer_group text, pattern text)`. Fan-out (Phase 6) consults
      bindings: only create a `deliveries` row for groups whose binding matches.
- [ ] Implement two matchers to learn the two styles: **topic**
      (`routing_key ~ pattern`, with `*`/`>`-style wildcards) and
      **header/content** (`headers @> '{...}'` JSONB containment).

**Lab:**
- [ ] Publish `orders.eu.created`; a group bound to `orders.*.created` gets a
      delivery row, one bound to `orders.us.>` does not. Routing works without
      the producer knowing any consumer exists.

**The aha:** routing is just a predicate evaluated at fan-out time. RabbitMQ's
"exchanges" are this; the flexibility lives in the matcher.

**Explain it back:**
1. Where does the routing decision execute, and why there rather than at claim
   time or produce time? What changes if a binding is added after events exist?
2. Topic-style vs header-style matching — when is each the right tool?

**Done when:** lab passes, NOTES.md, `git tag phase-7`.

**Real systems:** RabbitMQ exchanges — direct/**topic**/**headers**/fanout; NATS
**subjects** with `*` and `>` wildcards; Pulsar regex subscriptions.

---

## Phase 8 — Optional FIFO partitions

**Concept:** ordering on demand, paid for only where you opt in.

**Build:**
- [ ] Add `partition_key text` to `events` (nullable = no ordering).
- [ ] **Cursor consumers:** order within a partition by reading a partition with
      a single reader in `offset` order.
- [ ] **Lifecycle (delivery) consumers:** enforce "at most one in-flight per
      key" — claim with a predicate that **skips rows whose `partition_key`
      already has an in-flight delivery in this group**. Null key → no
      constraint, full concurrency.

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

**Explain it back:**
1. Why can't you have both total ordering and full concurrency? Where exactly
   does the serialization happen in your claim query?
2. What does a retry (Phase 2 backoff) do to per-key ordering? Is "at most one
   in-flight per key" enough to preserve order through a retry?

**Done when:** labs pass, NOTES.md, `git tag phase-8`.

**Real systems:** Kafka **partitions** (key → partition, order within
partition); Pulsar **Key_Shared** subscriptions (per-key order across multiple
consumers); SQS FIFO **MessageGroupId**.

---

## Phase 9 — Operational layer

**Concept:** what makes it survivable in production and observable enough to trust.

**Build:**
- [ ] **Retention policy:** a janitor that drops `events` older than X (and their
      `done` deliveries). Learn it the cheap way with native time-partitioning
      (`PARTITION BY RANGE (created_at)`) so retention is a partition `DROP`, not
      a mass `DELETE`.
- [ ] Optionally implement **log compaction** (keep only the latest event per
      `partition_key`) to see Kafka's compacted-topic idea.
- [ ] **Observability:** expose queue depth (`ready` count), lag per group, DLQ
      size, oldest unacked age. These four numbers are how you operate any queue.
- [ ] **Latency (optional):** add `LISTEN/NOTIFY` so producers wake idle workers
      instead of relying on poll interval, with a fallback poll for missed
      notifies and delayed (`run_at`) messages. Knowing *why* you keep the
      fallback poll (NOTIFY is fire-and-forget, lost if no listener) is the lesson.

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
`git tag phase-9`. The platform is done.

**Real systems:** Kafka `retention.ms` + **log compaction**; consumer **lag**
monitoring (Burrow); RabbitMQ queue-depth alarms; Pulsar tiered storage.

---

## How to use this

- **Do the phases in order.** Resist jumping to Phase 6. Each checkpoint is a
  concept you can't skip.
- **The labs are the learning.** Reading the aha is not the same as watching
  two workers not collide. If you skipped a lab, the phase isn't done.
- **Explain-it-back is the retention mechanism.** Answer from memory. Wrong or
  blank answers mean re-run the lab, not re-read the plan.
- **Tag every phase.** The diffs between tags are a record of *why* each
  refactor happened — that's the document your future self wants.
- **Stop when it's enough.** Phases 1–3 alone are a production-grade job queue.
  Phases 4–6 are where you graduate from "queue" to "log+queue platform."
  7–9 are polish.
- **The meta-lesson:** by Phase 6 you'll understand *in your hands* why Kafka,
  RabbitMQ, and Pulsar are different — they're the same primitives with different
  foundational defaults. That understanding is worth more than the code.
