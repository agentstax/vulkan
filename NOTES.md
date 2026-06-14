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

