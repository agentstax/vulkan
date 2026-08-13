---
status: accepted
date: 2026-07-08
phase: "8a"
---

# 0223 — Retention respects a MIN(committed) drop floor unless explicitly opted out

**Context.** Retention that drops or sweeps rows a lagging consumer group has
not yet processed silently loses messages: claims advance by id arithmetic
and a `SELECT` over a dropped range simply returns fewer rows, with no
in-band error.

**Decision.** `DropExpiredPartitions` and `SweepExpiredPartitions` both check
a floor — `cursorFloor`, the `MIN(committed)` across `cursor` rows — and
never remove rows past it. `WorkConsumerConfig.AllowDropPastCommitted`
(default `false`) is the explicit opt-in to Kafka's "lagging consumer falls
off the retention window" semantics; when set, it nils the floor out rather
than special-casing the SQL (the sweep's predicate is
`floor IS NULL OR id <= floor`).

**Consequences.** By default, a lagging group pins retention rather than
losing data — the failure mode is disk growth, which is observable, not
silent message loss, which is not. Opting out is a deliberate per-consumer
choice with a named flag, not an emergent behavior. The floor spans every
cursor sharing the log, which is what later motivated scoping it per topic
(see 0226, 0241).
