---
status: accepted
date: 2026-07-08
phase: "8a"
---

# 0222 — Retention is a hybrid: whole-partition drop plus a bounded sweep of survivors

**Context.** Dropping a whole expired partition removes rows without per-row
`DELETE` cost. But a low-volume log never fills a partition wide enough to
earn a drop, so drop alone would let expired rows sit forever.

**Decision.** `WorkConsumer.Janitor(ctx)` — a ticker loop spawned via
`errGroup.Go` in `Consume`, matching `RollWaterline`/`Project`'s shape — runs
three steps each tick: `EnsureNextPartition` (create-ahead),
`DropExpiredPartitions` (drop every whole partition whose newest row is past
`RetentionTTL`), and `SweepExpiredPartitions` (delete the ttl-expired prefix
off surviving partitions). The sweep never touches the active partition, and
deletes in `sweepBatch` calls — `DELETE ... WHERE created_at < cutoff ...
ORDER BY id ASC LIMIT batchSize`, plus the same batch's orphaned `deliveries`
rows, one transaction per batch — until a batch comes back short, so an
oversized backlog drains over several ticks instead of one giant
transaction. `dropPartition` deletes the partition's orphaned `deliveries`
rows and the partition itself in one transaction. `RetentionTTL` of zero
disables retention entirely: both drop and sweep no-op.

**Consequences.** Drop and sweep each cover exactly the volume range where
the other cannot fire: at real volume a partition ages out and is dropped
whole before the sweep walks far into it; at low volume no partition ever
fills, so the sweep is the only mechanism doing work — and its `DELETE` is
cheap precisely because low volume means few rows. Neither is a fallback for
the other; together they leave no gap.
