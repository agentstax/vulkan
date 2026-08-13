---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0378 — PartitionSafetyBuffer is deleted; the janitor always keeps the next partition created

**Context.** The knob's hardcoded `50000` default needed an actually-reasoned value. The sizing rule is `buffer >= produce rate x JanitorPollRate` — the janitor must win the race to the partition boundary within one 5s tick. 50k covered only 10k msgs/s against a measured saturated rate of ~130-145k msgs/s, and a fixed number is anti-correlated with need: high-throughput topics tune `PartitionSize` up, shrinking 50k to a ~0.4s sliver, while a small audit topic got always-ahead for free.

**Decision.** The knob is deleted entirely: field, `EnsureNextPartition`'s `safetyBuffer` param, and the `partition_safety_buffer` column (baseline DDL edited in place). `ensureNextPartition` unconditionally keeps head+1 created each tick. The derived default (`buffer = PartitionSize`, "the next partition always exists") made every value at or above `PartitionSize` equivalent and every value below it strictly worse — a knob whose whole settable range is "same or worse" is a footgun.

**Consequences.** Always-ahead costs nothing (verified on dev PG 17: `CREATE TABLE IF NOT EXISTS` on an existing partition short-circuits without locking the parent; an empty partition has no storage; retention treats empty as not-expired). A missed boundary is not free: measured ~8-14% slower with p99 doubled on the self-heal arm, and a real `CREATE ... PARTITION OF` takes ACCESS EXCLUSIVE on the parent at the worst possible moment. Precedent: pg_partman's `premake` keeps 4 partitions pre-created. Self-heal remains the correctness guarantee; create-ahead only decides how often the failure path fires.
