---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0383 — Partition-drop batch size is a fixed constant of 100, not a config knob

**Context.** Batched partition drops during `topic.Destroy` need a per-transaction batch size that stays safely under Postgres's shared lock table, whose stock ceiling is 64×100 = 6400 slots.

**Decision.** The batch size is a fixed constant of 100 partitions per transaction. Each partition pins about 5 lock-table slots (table, pkey index, partial compaction_key index, TOAST table, TOAST index), so 100 per transaction is ~500 slots — under 10% of the stock ceiling.

**Consequences.** No configuration surface to document or misuse; the margin is wide enough that non-stock lock-table settings don't need a knob. If a deployment ever shrinks the lock table drastically, the constant would need revisiting, but that is not a supported configuration.
