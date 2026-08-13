---
status: accepted
date: 2026-06-20
phase: "3.5"
---

# 0086 — The knob is set via ALTER DATABASE, and reset to on after the sweep

**Context.** The benchmark drives Postgres through a pgx connection pool. A session-level `SET synchronous_commit=off` applies only to the session that issued it and would never reach the pool's other connections, silently benchmarking a mix of settings.

**Decision.** Set the knob with `ALTER DATABASE example_db SET synchronous_commit=off`, which every pool connection inherits at connect time; after the sweep, `RESET` it back to the default `on`.

**Consequences.** Sweep results reflect a uniform setting across all connections. The dev database is not left silently non-durable — adopting `off` later is a deliberate re-run of the same `ALTER`, not a leftover. **Rejected:** session-level `SET` — it does not propagate to pooled connections.
