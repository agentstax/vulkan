---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0263 — The latest-per-key predicate is unbounded, not bounded by the claim's own high

**Context.** The predicate was originally planned as bounded — the max `id` for a key at or below the claim's own `high`, mirroring how a claim's range is fixed. But a lease's `high` is pinned once and reused identically on every reclaim: after a crash, a newer same-key write landing outside that frozen window would be invisible to a bounded check, so the reclaimed row would look locally latest while actually superseded — and end up with zero completed delivery attempts instead of at least one.

**Decision.** The predicate is unbounded: "no row with a higher `id` and the same `compaction_key` exists, anywhere," re-evaluated live against current state on every read, including reclaims. The guarantee being upheld is "the current latest value gets delivered, eventually" — what Kafka documents for its own compacted topics — not "every version of a key gets delivered."

**Consequences.** `readMessages` (CURSOR) and `FanOut` (LIFECYCLE) carry the identical predicate, not merely same-shaped ones; `FanOut` was never bounded by a claim high anyway, so unbounded fits it better than a bounded version would have. **Rejected:** bounding by the claim's `high` — wrong exactly and only on crash/reclaim.
