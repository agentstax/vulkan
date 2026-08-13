---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0327 — Commit's per-exception writes are batched; RecordException stays one message at a time

**Context.** Write-path round trips were audited per function rather than batched wholesale. Two candidates: the loop in `Commit`/`PartialCommit` that records exception and terminal rows (it runs after every `consumerFunc` call has already finished, inside an already-open transaction), and `RecordException*`/`Record*`'s one-message-at-a-time calls.

**Decision.** The `Commit`/`PartialCommit` loop collapsed from 2 sequential round trips per recorded message to one `pgx.Batch`. `RecordException*`/`Record*` were deliberately left unbatched.

**Consequences.** Measured: recording N exceptions went from 3.16ms at N=1 to 9.3ms at N=1000 — total wall-clock grew ~3x while N grew 1000x. **Rejected:** batching `RecordException*`/`Record*` — it would defer the durable write past multiple `consumerFunc` calls, trading a durability/idempotency guarantee for a performance win nothing asked for.
