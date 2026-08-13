---
status: accepted
date: 2026-06-30
phase: "6.5b"
---

# 0164 — Reclaim drains before fresh claim, one expired lease per poll

**Context.** With leases in place a poll could either pick up new frontier
work or recover a crashed worker's expired lease; the order determines how
long an unresolved gap pins the waterline.

**Decision.** `ClaimMessagesWithCursor` runs `ReclaimWithCursor` first — take
one expired lease (`until < now()`, `LIMIT 1 FOR UPDATE SKIP LOCKED`) and
re-read its exact `(low, high]` range under a fresh token — and falls through
to `FreshClaimMessagesWithCursor` only when nothing is reclaimable.

**Consequences.** A crashed range is older work; draining it first keeps
`committed` moving and bounds how far `claimed` runs ahead of an unresolved
gap. One reclaim per poll is enough — a backlog of expired leases bleeds down
across successive polls. The original delete-then-insert reclaim shape was
later replaced by a single atomic UPDATE (see 0190); the reclaim-before-claim
ordering stands.
