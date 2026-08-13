---
status: accepted
date: 2026-07-08
phase: "8a"
---

# 0227 — Partition automation is the Janitor loop in Go, not pg_partman

**Context.** Partition create-ahead, expiry judgment, and dropping need
automation; pg_partman is the standard Postgres extension for exactly this.

**Decision.** No pg_partman, no extensions. Declarative partitioning is core
Postgres; pg_partman is only automation around
`CREATE TABLE ... PARTITION OF` and `DROP TABLE`, and `WorkConsumer.Janitor`
is that same automation, in Go, on a ticker.

**Consequences.** The system runs on stock Postgres with no extension
install or upgrade surface. Partition lifecycle policy (width, TTL, floor)
lives in application config next to the code that enforces it, at the cost
of owning the automation loop instead of delegating it.
