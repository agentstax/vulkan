---
status: accepted
date: 2026-06-14
phase: "1.5"
---

# 0021 — The queue lives in the same Postgres database as the business data

**Context.** Writing a business row and publishing an event to a separate
broker is the dual-write problem: no ordering is safe (DB commits then publish
fails → event lost; publish succeeds then DB rolls back → phantom event), and
retries only narrow the window — a crash between the two writes, or a
permanent rejection of the second, leaves them permanently inconsistent.

**Decision.** `message_log` is a table in the business database, so the
enqueue `INSERT` and the business write commit in one transaction — both land
or neither does.

**Consequences.** No reconciliation code exists because no inconsistent state
can. Isolation means a consumer can never claim a message from an uncommitted
producer transaction. `message_log` is also a working outbox table; only a
relay is missing to forward events to external systems. The cost is coupling:
the queue shares the business database's connection and transaction budget and
cannot be scaled or operated independently.
**Rejected:** external broker (Kafka/RabbitMQ) — the transaction boundary
cannot reach a separate system.
