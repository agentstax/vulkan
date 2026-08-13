---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0372 — Janitor and partition tuning fields are topic-scoped and persisted on the topic row

**Context.** Config placement was audited field by field with a concrete test, not a vibe: trace which datastore call each `MessageConsumerConfig` field feeds, and check whether that call takes `consumerGroup` (genuinely per-group) or only `topicID` (shared topic state, misplaced).

**Decision.** `PartitionSafetyBuffer`, `JanitorPollRate`, and `JanitorSweepBatchSize` moved to `topic.Config`/`topic.Topic` and were persisted as new `topic` table columns (folded into the baseline migration in place). The smoking gun: `EnsureNextPartition`/`DropExpiredPartitions`/`SweepExpiredPartitions`/`SweepExpiredIdempotencyKeys` already took five sibling topic-scoped inputs in the same calls — these three were stragglers. Persisting (not just moving the Go field) is the point: a transient config field would let two independently-constructed consumer-group processes disagree, the exact bug being fixed. `WaterlinePollRate` stays on `MessageConsumerConfig` — `AdvanceWaterline(topicID, consumerGroup)` genuinely takes a group; each group's cursor is independent. `JanitorPollRate` dropped its "0 defers to ClaimPollRate" fallback and got its own real default, 5s.

**Consequences.** Every consumer process on a topic now agrees on janitor behavior. `QueueTimeout` was deliberately left unplaced pending the buffered-claim work. `PartitionSafetyBuffer` was later deleted outright [0378].
