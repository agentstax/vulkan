---
status: accepted
date: 2026-06-30
phase: "6.5b"
---

# 0161 — Crash recovery rides on a lease per claimed range, not per-message rows

**Context.** On the cursor path a worker that claims a range and crashes before
finishing strands `(committed, claimed]`: the next claim reads above `claimed`
and skips the gap, and a naive waterline would advance right over it. The
happy path deliberately writes zero rows per successful message, so there is
no per-message state to lean on for recovery.

**Decision.** `ClaimMessages` INSERTs a `lease(token, consumer_group, low,
high, until)` row — PK `(token, consumer_group)`, `token UUID DEFAULT
gen_random_uuid()` — in the same transaction as the `claimed` advance, and
returns `ClaimedRange{Lease, Messages}`. A crash leaves an expired lease that
another worker reclaims and re-reads under a fresh token.
`CommitRange(group, token)` is a token-guarded `DELETE FROM lease`: it frees a
finished range and no-ops if the worker was reclaimed in the meantime. No
`deliveries` rows are written for crash recovery.

**Consequences.** Every in-flight offset belongs to exactly one lease, and the
waterline cannot pass an offset until its lease is freed — crash-safety,
reclaim, and the waterline pin are all reads of the same ownership fact.
Reclaimed ranges are reprocessed, so delivery is at-least-once and processing
must be idempotent. The zero-writes-per-success property of the happy path is
preserved.
