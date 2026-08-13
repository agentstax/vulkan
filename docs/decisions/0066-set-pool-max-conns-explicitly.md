---
status: accepted
date: 2026-06-20
phase: "3"
---

# 0066 — MaxConns is set explicitly on the datastore pool

**Context.** pgxpool's default `pool_max_conns` is `max(4, nCPU)` — 10 on this box. With one connection per concurrent per-message commit, any worker count above ~10 silently queues on the pool rather than on Postgres.

**Decision.** Expose and set `MaxConns` (`pool_max_conns`) on the datastore so the pool scales with the configured worker count.

**Consequences.** Removes a fake ceiling that would otherwise masquerade as a database limit in throughput measurements; worker-count sweeps above 10 become meaningful. The pool size becomes a knob that must track worker configuration.
