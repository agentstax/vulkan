---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0350 — PartitionSize is immutable by omission from AlterConfig; dynamic partition bounds deferred

**Context.** Partitions are created `FOR VALUES FROM (n*size) TO ((n+1)*size)`, and the runtime computes partition identity from `head/partitionSize` at every hot site — producer `ensureCoveringPartition`, consumer `EnsureNextPartition`, `DropExpiredPartitions`, `dropPartition`'s delivery-cleanup range, sweep batching. Name and bounds are the same number viewed through `size`; change `size` mid-life and `head/size` names a partition whose real on-disk bounds no longer match — wrong partition dropped, overlapping-partition CREATE errors, a message routed to a table that does not cover its id. Dated approximately; built across July 2026.

**Decision.** `PartitionSize` is simply absent from `AlterConfig` — the type system enforces immutability, not a runtime "cannot change this" check.

**Consequences.** The settled future unlock, deliberately deferred: stop computing bounds and read the real ones from `pg_inherits` + `pg_get_expr(relpartbound)`; keep `_<n>` naming but never reconstruct a name from math, using the `(relname, lower, upper)` triples as handed back; mint each new partition at `from = max existing upper bound, to = from + current size`. That yields Kafka `segment.bytes` semantics — altering size affects only future partitions — and is fully backward-compatible since existing bounds already sit in the catalog. Deferred because it rewrites every hot partition-math path for an admin nicety. **Rejected:** a runtime immutability check — an absent field cannot even be misused.
