---
status: accepted
date: 2026-06-20
phase: "3"
---

# 0065 — The claim index is a partial index on id covering ready and processing

**Context.** The claim query does `ORDER BY id`, so the planner takes the primary key and filters inline, scanning every terminal `done`/`dead` row that accumulates ahead of the live set. On a table with 150k `done` and 50k `ready` rows the claim degraded from 0.057ms (fresh table) to 41.8ms — 730×. The existing 004 partial indexes (`can_run_after WHERE status='ready'`, `lease_until WHERE status='processing'`) cannot drive an `id` ordering across the `ready OR processing` predicate, so the claim never uses them.

**Decision.** Migration 005 adds `idx_claim_active (id) WHERE status IN ('ready','processing')` — a partial index keyed on `id` covering both live states, so the ordered scan skips terminal rows entirely.

**Consequences.** Claim time recovered to ~0.09ms and deep-backlog throughput from ~4.8k to ~19k msgs/s. The index contains only live rows, so it stays small no matter how much history accumulates. Open question carried forward: whether the unused 004 partial indexes still earn their keep. **Rejected:** a full composite `(status, run_at)` index — it indexes every terminal row forever (bloat, cache pressure, vacuum cost), and low-cardinality `status` is a poor leading column.
