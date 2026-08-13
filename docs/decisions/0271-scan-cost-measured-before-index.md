---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0271 — The correlated scan's cost was measured before committing to latest_key

**Context.** The claim-time filter could have shipped on the correlated "no higher id with this key exists" scan alone; adding a synchronously maintained lookup table is schema, write-path, and read-path work that should not rest on an assumed performance problem.

**Decision.** Measure first: `compactionwidthlab` established the shape — a never-superseded key's negative proof touches every partition, while a just-superseded key's positive proof benefits from runtime partition pruning and early termination — and `compactionscalelab` established the growth curve: linear, roughly 10µs per partition, no amortization, extrapolating to about a second per surviving key on a 100K-partition topic's backlog replay. Only with that evidence was the `latest_key` schema/write-path/read-path sequence committed to.

**Consequences.** The index is justified by recorded numbers, not a guess. The two measurement labs are deliberately frozen historical records of the old scan; their job was done once the decision landed.
