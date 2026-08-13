---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0286 — Graceful shutdown narrows the interrupted lease instead of freeing it

**Context.** A consumer cancelled mid-range has processed a prefix of its claimed messages. Freeing the lease outright would redeliver the resolved prefix; inventing a new expiry mechanism for the unprocessed suffix would duplicate crash recovery.

**Decision.** `CursorClaim`'s per-message loop checks `ctx.Done()` between messages (mid-message interruption is the hard per-message timeout, a separate mechanism). On interruption, a new datastore method `PartialCommit` narrows the lease's low bound — `UPDATE lease SET low = $lastProcessed` — keeping the same token and same `until`, so the untouched suffix rides the existing crash-recovery reclaim path. The write goes through `context.WithTimeout(context.WithoutCancel(ctx), AckMargin)`, the same "extra time to record outcomes" pattern `ShutdownFunc` already uses, since the ctx that triggered the interruption is already `Done`.

**Consequences.** The resolved prefix is never redelivered; the suffix needs no new expiry machinery; the waterline's exception-blocker and lease-narrowing terms combine via `LEAST`. **Rejected:** freeing the lease outright — redelivers work already done.
