---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0421 — One generic worker/worker_instance pair replaces per-feature maintenance/duty plumbing

**Context.** Every background loop (janitor, waterline advance, cron scheduler, the consumption loops) needed an answer to "what should run, and who is running it," and each had grown its own maintenance/duty plumbing.

**Decision.** One generic mechanism. A `worker` row is the spec: name, exactly one owner, metadata, `target_instances`. A `worker_instance` row is a live claim: a token plus a heartbeat-renewed `expires_at`. The manager runs one reconcile loop that spawns instances up to `target_instances`. Suspending any loop anywhere is setting `target_instances` to 0. Built as pkg/worker with the vocabulary/controller/datastore layering; the DDL landed as a baseline edit.

**Consequences.** Liveness is lease-shaped: a crashed instance's row lingers until its lease expires, so failover latency is bounded by the instance TTL rather than the next tick, and the machinery must treat a lost instance as normal operation, not an error. Every later feature (the daemon, deployment-wide scope, uniform suspend) rides this one table pair instead of new plumbing.
