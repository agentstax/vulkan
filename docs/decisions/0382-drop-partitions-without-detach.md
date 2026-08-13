---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0382 — Partition batches use plain DROP TABLE IF EXISTS with no DETACH first

**Context.** The designed fix for `topic.Destroy` lock exhaustion was worded as "batched DETACH + DROP per partition." Implementing it raised the question of whether detaching before dropping actually buys anything.

**Decision.** `dropPartitionBatch` does a plain `DROP TABLE IF EXISTS` per partition with no detach. A non-concurrent `DETACH` takes the exact same ACCESS EXCLUSIVE lock on the parent that the `DROP` does, so detaching first is the same locks in two statements instead of one.

**Consequences.** One statement per partition, half the lock acquisitions of the two-step shape. **Rejected:** `DETACH CONCURRENTLY` — cannot run inside a transaction block and leaves pending-detach states that would complicate crash resume.
