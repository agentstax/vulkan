---
status: accepted
date: 2026-06-15
phase: "2"
---

# 0041 — The claim is row data (status + locked_at), not a held DB lock

**Context.** Holding `FOR UPDATE` for the whole processing duration pins a
transaction and a connection per in-flight message; a 10-minute job holds both
for 10 minutes, which does not survive real concurrency.

**Decision.** A message carries its state machine in its own columns
(`status`, `attempts`, `can_run_after`, `locked_at`, `last_error`). Claiming
stops deleting: a single `UPDATE ... RETURNING` with `FOR UPDATE SKIP LOCKED`
in its subquery flips `status='ready'` → `'processing'`, stamps `locked_at`,
and increments `attempts`. The `FOR UPDATE` lock now spans only that claim
statement — milliseconds. On success `RecordSuccess` moves the row to `done`.

**Consequences.** Long jobs no longer pin transactions or connections. The
durable "I'm working on this" is `status='processing'` plus `locked_at`, so
the auto-releasing lock is gone: reclaiming crashed work becomes the queue's
own responsibility (the lease), and that reclamation is what turns delivery
into at-least-once.
