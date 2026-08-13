---
status: accepted
date: 2026-06-23
phase: "4"
---

# 0101 — Consuming reads an append-only log through a per-group cursor instead of mutating message rows

**Context.** The original design made consumption destructive or stateful per row: consume meant `DELETE`, then later `UPDATE status` plus `attempts`/`lease_*` columns and reclaim machinery. That made retention and replay impossible — history was destroyed or overwritten as it was processed.

**Decision.** Split the data in two: `message_log` (`id BIGSERIAL`, `payload`, `created_at`; append-only, rows never mutated or deleted) and `cursor` (`consumer_group`, `position`) — one high-water mark per group. Claiming is a pure read (`WHERE id > position ORDER BY id LIMIT N`); after processing, the consumer advances its cursor with `MoveCursor`. The per-row lifecycle columns are dropped from the hot path.

**Consequences.** Retention and replay become free: the log is never destroyed, so replay is just resetting `position`. The cost is per-row resolution — the cursor is a single integer, so "message 5 failed but 6, 7, 8 are done" is inexpressible; a failure can only stop the cursor, not punch a hole in it. That gap is what the later sparse exception side-table exists to fill. The high-water mark is only correct over an ordered claim, which becomes its own invariant.
