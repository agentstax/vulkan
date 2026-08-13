---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0324 — delivery_log_<topic_id> records only failed attempts; a row's absence is the success signal

**Context.** An attempt audit trail was wanted, but nothing on a hot path should pay for it, and successes vastly outnumber failures.

**Decision.** A per-topic `delivery_log_<topic_id>` table (`consumer_group, message_id, attempt, attempted_at, error`) records only FAILED attempts, written in the same transaction/statement as the `delivery_<topic_id>` mutation it shadows. A row's absence for a given attempt IS the "succeeded" signal, so nothing on any hot path ever reads the table. Retries append distinct rows rather than overwriting, and both retention paths drain it.

**Consequences.** Success costs zero audit writes; the log is pure history, never control flow. As first built, `DisableDeliveryLog` skipped creating the table entirely; that part was later replaced — the table is now always created and the flag gates only the writes, so every topic shares one schema shape.
