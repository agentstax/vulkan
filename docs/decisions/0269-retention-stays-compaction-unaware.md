---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0269 — Retention stays compaction-unaware; janitors garbage-collect dangling latest_key rows

**Context.** With retention and compaction combined, a dormant key's last surviving version eventually ages past `RetentionTTL`. The question was whether `dropPartition`/`sweepBatch` need a gate to preserve a key's final row forever.

**Decision.** No gate — a dormant key's last surviving row aging out is intentional expiration, not a bug. "Retention" means "a key untouched longer than the TTL window ages out," matching Kafka's own `cleanup.policy=compact,delete` and DynamoDB TTL. Safety falls out of an existing invariant: id order tracks time order and compaction always keeps the highest-id row, so a key touched inside the TTL window keeps landing in fresh partitions immune to both retention paths. The one real gap was cleanup, not prevention: `dropPartition` and `sweepBatch` each garbage-collect the now-dangling `latest_key` row when reaping a key's last surviving version, mirroring their existing `deliveries`-cleanup precedent exactly (range-based delete in `dropPartition`, `ANY(ids)`-based in `sweepBatch`).

**Consequences.** `RetentionTTL` keeps a single meaning for keyed and unkeyed messages alike; `latest_key` never accumulates rows for reaped keys (proven by `latestkeysretentionlab`). **Rejected:** a compaction-aware gate in the janitors — it would make `RetentionTTL` mean two different things depending on whether a message carries a `compaction_key`.
