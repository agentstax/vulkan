---
status: accepted
date: 2026-07-14
phase: "10"
---

# 0303 — The metrics snapshot is the single source for both the debug readout and the OTel instruments

**Context.** Two consumers of "current queue state" shipped at once: a human-readable debug readout (`String()`, scoped per `(group, topic)`) and the OTel instruments. Two independent implementations of the same numbers would drift apart.

**Decision.** One snapshot struct/method merges the queue-state query's DB-truth numbers with the in-process `AbandonedRoutines` counters; the debug readout and every OTel instrument read from that snapshot, and neither computes its own numbers.

**Consequences.** The readout is guaranteed to show exactly the data the instruments export to machines — it is the free human-readable consumer of the identical snapshot. Any new health number is added once, in the snapshot, and appears on both surfaces.
