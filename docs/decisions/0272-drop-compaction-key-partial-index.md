---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0272 — The compaction_key partial index was dropped once latest_key left it without a consumer

**Context.** After the read path swapped from the correlated scan to the `latest_key` lookup, the per-topic partial index `(compaction_key, id) WHERE compaction_key IS NOT NULL` existed to serve a query that no longer ran.

**Decision.** Drop it — but only after verifying via `EXPLAIN`, twice (a keyed row present, and a pure-unkeyed range matching `compactionlab`'s own shape), that the index appears nowhere in the new predicate's plan, not even as a rejected candidate, and that the unkeyed-row short-circuit holds identically against `latest_key`.

**Consequences.** Keyed publishes stop paying maintenance on a dead index. The verification caught a real staleness bug: `compactionlab`'s `EXPLAIN` check had silently drifted to testing the old query after the read-path swap, its comment still claiming to test the exact shape `readMessages` runs — fixed before deciding. Cost accepted: `compactionwidthlab`/`compactionscalelab`, the index's only remaining consumers, can no longer exactly reproduce their recorded historical numbers on a re-run — acceptable, since those measurements were deliberately frozen.
