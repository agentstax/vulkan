---
status: accepted
date: 2026-07-14
phase: "10"
---

# 0301 — The waterline rollup stays lazy; no cursor update at commit time

**Context.** `Commit` today touches only `lease` and `deliveries`, never `cursor`; a lazy background poll (`AdvanceWaterline`) moves `committed` afterward, so retention drops and delivery sweeps trail real progress by up to the poll interval (a few seconds under the default `ClaimPollRate`). The alternative was updating `cursor` synchronously inside every `Commit`. The question had been deliberately deferred earlier and was resolved here by measurement.

**Decision.** Stay lazy: `AdvanceWaterline` remains a periodic background poll and `Commit` never touches `cursor`. Added `WorkConsumerConfig.WaterlinePollRate` (0 defaults to `ClaimPollRate`, same pattern as `JanitorPollRate`) so staleness is tunable without adding hot-path cost.

**Consequences.** Cursor-derived actions lag by up to `WaterlinePollRate`; an operator who needs less lag shortens the knob — a representative lab run measured catch-up time drop 15.6x (2.03s to 130ms) with a faster poll. Zero throughput cost preserved: concurrent committers in a group keep committing fully in parallel. **Rejected:** synchronous `UPDATE cursor` in `Commit` — it adds a row every concurrent committer in the group serializes on, measured 1.3x-1.9x slower at 20 concurrent committers; a permanent tax on every commit to buy a sub-5-second wait down to milliseconds, which retention drop decisions and a debug readout do not need.
