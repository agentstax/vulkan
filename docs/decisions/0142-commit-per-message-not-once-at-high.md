---
status: accepted
date: 2026-06-26
phase: "6.5a"
---

# 0142 — `committed` advances after each message, not once per batch at `high`

**Context.** After claiming the range `(low, high]`, the consumer could advance `committed` once to `high` (one update per batch, the O(1) ideal) or after each processed message (N updates per batch).

**Decision.** Advance `committed` after each message — the same per-message granularity chosen when the cursor was a single `position`.

**Consequences.** A crash reprocesses only from the last committed message, not the whole range. **Rejected:** committing once at `high` — cheaper and the headline write-amplification ideal, but a mid-range crash would replay the entire batch. The choice dilutes the O(1)-per-batch win, yet each update is one in-place HOT update on a single `cursor` row, still far below a per-message INSERT. Benchmarking was deliberately not run at this point, so the exact ratio against the `deliveries` baseline is unrecorded by choice.
