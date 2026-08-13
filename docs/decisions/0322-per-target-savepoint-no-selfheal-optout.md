---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0322 — Partition self-heal in ProduceInTx is per-target via SAVEPOINT, with no opt-out

**Context.** Postgres aborts the whole transaction on the first statement error, so inside a multi-target transaction, one target's missing-partition retry must not poison the other targets' work — isolation requires a savepoint established before the risky insert runs.

**Decision.** `ProduceInTx` wraps its own `producerFunc` call plus insert in one savepoint, with the insert and `RELEASE` batched via `pgx.Batch`. There is no `SkipPartitionSelfHeal` option.

**Consequences.** A forced missing-partition retry on one target never touches another target's insert or reruns a side effect between them. Measured cost: +55-95% sequential per-call latency once batched, but 91-105% of baseline aggregate throughput under concurrency — the savepoint tax does not cost real throughput. Throughput-sensitive users are pointed at `JanitorPollRate`/`PartitionSize` instead of an escape hatch. **Rejected:** mirroring `SkipIdempotency`'s protected-by-default-with-opt-out shape — `SkipIdempotency` is opted into for an orthogonal reason (tolerating duplicates), whereas the population reaching for a self-heal opt-out under throughput pressure is exactly the population most likely to outrun the janitor's create-ahead and need the protection.
