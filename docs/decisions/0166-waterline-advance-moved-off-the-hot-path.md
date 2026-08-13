---
status: accepted
date: 2026-06-30
phase: "6.5b"
---

# 0166 — Waterline advance moved to a lazy roller; per-message MoveCursor deleted

**Context.** The first cursor-path implementation advanced `committed` per
message on the hot path via `MoveCursor`. With ranges owned by leases,
`committed` can no longer be advanced message-by-message anyway — it must
respect open leases.

**Decision.** All waterline motion moved to `RollWaterline`, a dedicated
goroutine that ticks on `PollRate` and calls `AdvanceWaterline` off the hot
path. `CursorClaim` claims a `ClaimedRange`, processes the whole range, and
calls `CommitRange(token)`, which only frees the lease; the hot path never
touches `committed`. `MoveCursor` was deleted.

**Consequences.** `committed` lags in-flight reality by up to one roller tick
— acceptable because it is a durability frontier, not a scheduling input.
Success costs zero writes to the cursor row per message; the cursor row is
contended only by the roller and the claim's `claimed` advance.
