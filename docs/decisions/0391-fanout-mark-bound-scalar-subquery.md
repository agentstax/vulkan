---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0391 — FanOut's scan bound is a scalar subquery, not a join on old_values

**Context.** FanOut's materializing statement scans `id > committed` from the mark held in the `old_values` CTE. How that bound reaches the scan changes the plan shape entirely.

**Decision.** The bound is written as a scalar subquery — `id > (SELECT committed FROM old_values)` — never as a join against `old_values`. Joining plans the bound as a join FILTER that walks the whole index from 0; measured 660x slower at 200k rows.

**Consequences.** The scalar-subquery form lets the planner use the bound as an index start condition, keeping ticks flat regardless of log size. Any edit to this query must preserve the scalar-subquery shape; the join form is a silent performance cliff that only shows on large logs.
