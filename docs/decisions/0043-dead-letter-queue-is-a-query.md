---
status: accepted
date: 2026-06-15
phase: "2"
---

# 0043 — The dead-letter queue is WHERE status='dead', not a separate table

**Context.** Messages that exhaust their attempts need somewhere to go for
inspection and possible replay; brokers typically move them to a separate
dead-letter destination.

**Decision.** Dead rows stay in `message_log` with `status='dead'`; the
dead-letter queue is the query `WHERE status='dead'`.

**Consequences.** No move/copy machinery and no second table to keep in sync —
the terminal state is just another value of the same `status` column, and
inspection is a plain `SELECT`. Dead rows share the table with live traffic,
so they occupy the same storage and indexes until cleaned up.
