---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0388 — The cursor-claim query locks the cursor row with FOR UPDATE in the old_values read

**Context.** A code comment questioned whether the cursor-read transaction should lock rows. It was a live race, proven with a two-connection interleave: claimer A takes (0,100] and holds its transaction open; claimer B (same group) snapshots and blocks on A's cursor-row lock; A commits. Under READ COMMITTED, B's UPDATE re-evaluates against the row's latest version (EvalPlanQual), so `high` computes correctly off claimed=100, but the `old_values` CTE stays materialized from B's original snapshot, so `RETURNING old_values.claimed AS low` hands back the stale 0 — B returns (0,200], overlapping the range already leased to A. Systematic double-delivery whenever same-group workers race on a deep backlog; same READ COMMITTED/EvalPlanQual anomaly family as the AdvanceWaterline snapshot race.

**Decision.** Add `FOR UPDATE` to the `old_values` select. A locking read does not answer from the snapshot: B blocks at the CTE and, once A commits, reads the latest committed row, so low and high come from the same row version. Repro rerun with the lock: A gets (0,100], B gets (100,200], disjoint.

**Consequences.** Cost measured as zero — the UPDATE was acquiring that row lock anyway; `FOR UPDATE` just moves the acquisition before the read, and cursorbench numbers are identical within noise (claiming avg 661µs vs 622µs at 500 partitions). The only new locking is on idle ticks (caught-up pollers briefly lock the cursor row), invisible at poll-rate frequencies. The stale-`head` question was checked and is benign in both shapes: one statement is one snapshot, EvalPlanQual recomputes neither an inline scalar subquery nor a CTE, and a stale head just means the claim lags one poll tick.
