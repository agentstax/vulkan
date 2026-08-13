---
status: accepted
date: 2026-06-15
phase: "2"
---

# 0044 — Lease reclamation is a claim-predicate branch, not a reaper daemon

**Context.** With the claim held as row data instead of a DB lock, a worker
that crashes mid-process leaves its row stuck at `status='processing'` with no
lock for Postgres to release — something has to make that row claimable again.

**Decision.** An expired lease is just another claimable row: the claim's
`WHERE` gains the branch `OR (status='processing' AND locked_at < now() -
buffer)`. The next ordinary claim matches the stuck row, re-stamps
`locked_at`, and increments `attempts`. No separate reaper process exists.

**Consequences.** The design stays coordinator-free — every worker is
symmetric, with no singleton sweeper to deploy, schedule, or fail over.
Reclamation happens exactly as often as claims do, and a reclaimed message is
a second delivery, which is what makes the contract at-least-once.
**Rejected:** a dedicated reaper daemon — a coordinator and a second code path
for a fact the claim predicate already expresses.
