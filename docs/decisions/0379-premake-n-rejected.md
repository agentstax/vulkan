---
status: rejected
date: 2026-07-28
phase: "13"
---

# 0379 — Multi-partition create-ahead (a PartitionsAhead knob) is rejected; PartitionSize is the lever

**Context.** After the janitor moved to always keeping head+1 created, pg_partman's `premake N` (keep N partitions pre-created) was considered as the next step. One-ahead plus self-heal is the design, not an interim state.

**Decision.** Rejected. The conditions that make `premake 4` right for pg_partman are all absent here: (a) their maintenance runs from cron (hourly/daily), so slack is measured in missed maintenance windows — the janitor ticks every 5s, and one-ahead already covers `PartitionSize / JanitorPollRate` = 200k msgs/s sustained at defaults, above the measured ~130-145k/s ceiling; (b) their time-based partitions burn down with the clock whether or not traffic arrives — id-based partitions fill only with traffic, so the requirement is rate times poll interval, not wall-time slack; (c) their miss is a disaster (rows spill into the default partition, manual repartitioning to repair) — this system's miss is a measured ~10%/p99-doubling self-heal blip.

**Consequences.** A `PartitionsAhead` knob would re-create the exact disease just deleted: at every achievable rate, every value above 1 is indistinguishable in practice. If the janitor genuinely cannot keep up, the correct existing lever is `PartitionSize` — coverage scales linearly with it, and users at that scale must reason about it anyway for retention granularity. Multi-ahead stays the known additive escape hatch only for a future produce path fast enough to beat 200k/s sustained (e.g. COPY-based ingest), added with a measured need in hand.
