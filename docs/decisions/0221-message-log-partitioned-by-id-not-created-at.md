---
status: accepted
date: 2026-07-08
phase: "8a"
---

# 0221 — message_log is partitioned by RANGE (id), not created_at

**Context.** Retention is time-based, which suggests partitioning `message_log`
by `created_at`. But every hot query — the claim range, `MAX(id)`, the
lifecycle join — filters by `id`, and none by `created_at`. The planner can
only prune partitions using columns present in the `WHERE`.

**Decision.** `message_log` is `PARTITION BY RANGE (id)`, folded into its
original `CREATE TABLE` migration, which also creates `message_log_0` so the
table is insertable before any janitor runs. Partition width is a config knob
(`WorkConsumerConfig.PartitionSize`), not hardcoded past the migration's
first-partition bound. Retention stays decidable per partition from `id`
alone because the table is append-only, so ids are approximately
time-ordered; a partition's age is read from its newest row via
`ORDER BY id DESC LIMIT 1`, riding the PK index — no `created_at` index
needed.

**Consequences.** Claims and other id-range reads prune to the 1–2 partitions
they actually touch. "Old enough to drop" is judged per partition by the
timestamp of its newest row.
**Rejected:** partitioning by `created_at` — Postgres requires the partition
key inside any primary key, so the PK would widen to `(id, created_at)` for
no querying benefit, and id-range claims could not be pruned at all: with a
year of daily partitions, every claim would probe all 365.
