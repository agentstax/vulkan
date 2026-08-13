---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0489 — Checks declare themselves at `RegisterSystem`, with binds made idempotent by `UNIQUE` + `ON CONFLICT DO NOTHING`

**Context.** The default checks must exist on every deployment without an operator installing them, and system registration runs on every boot, so whatever wires the checks up must be safe to repeat.

**Decision.** Each check's `Declare` runs at `RegisterSystem`: it registers the check's cron job and binds its consumer group to its job name, with the bind made idempotent by the binding table's `UNIQUE` constraint plus `ON CONFLICT DO NOTHING`. The check's worker definitions join the manager beside janitor, cronscheduler, and waterline.

**Consequences.** A fresh database gets the full default-alert setup from a normal boot, and repeated boots write nothing new. Idempotency lives in the schema, not in read-then-write application logic.
