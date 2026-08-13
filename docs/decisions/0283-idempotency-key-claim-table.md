---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0283 — An idempotency_key claim table prevents double-publish on ambiguous commit acks

**Context.** Making `AppendMessage` retryable surfaced a correctness gap beyond "just retry": when a commit lands but its acknowledgement is lost, the retry re-runs the insert and publishes the message twice.

**Decision.** An `idempotency_key` claim table, written with `ON CONFLICT DO NOTHING` and checked before the `message_log` insert, so a retried publish with the same key becomes a no-op. Claimed keys are swept on `Topic.IdempotencyKeyTTL`, defaulting to 24h, with deliberately no "0 = forever" escape hatch.

**Consequences.** Exactly-once holds under a retried key; distinct keys never collide; the table's size is bounded by the sweep rather than growing forever. Producers wanting the old behavior must opt out explicitly per call (recorded separately with the cost measurements).
