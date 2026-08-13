---
status: superseded
date: 2026-07-08
phase: "8a"
---

# 0224 — Partition labs swap message_log to a lab-scale width and restore the schema on exit

**Context.** `message_log_0`'s real 1,000,000-row partition width makes
multi-partition demos impractical at lab scale, but the partition-pruning and
drop-floor labs need several partitions to prove anything.

**Decision.** `partitionlab` and `dropfloorlab` drop and recreate
`message_log` at a lab-scale partition width for their own run, then restore
the migration's exact shape on exit. This permanently discards whatever rows
were in `message_log` each time either lab runs — schema is restored, data is
not — accepted as safe because no FK ties `message_log` to
`cursor`/`deliveries`/`lease`. `sweeplab` needs no swap, since staying inside
one never-rolled partition is exactly the condition the sweep exists to
cover; it runs with `AllowDropPastCommitted=true` throughout because the
drop floor is global across every group sharing `message_log`, so leftover
cursor rows from other labs would otherwise couple its behavior to theirs.

**Consequences.** The labs prove automated pruning against the real datastore
methods, at the cost of wiping `message_log` on every run.
**Rejected:** a real migration-width config knob — deferred rather than added
for a lab's sake.
**Rejected:** skipping the automated pruning proof outright.
**Superseded** by the per-topic tables split (0241): a lab-scale partition
width became just a `PartitionSize` passed to `topic.Register`, and the
drop-and-recreate schema-swap was deleted entirely.
