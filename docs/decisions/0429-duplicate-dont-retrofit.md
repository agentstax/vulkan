---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0429 — Duplicate beside pkg/maintain, don't retrofit it

**Context.** Migrating every background loop from the maintenance/duty machinery to worker rows could either convert pkg/maintain in place or build the replacement alongside it.

**Decision.** pkg/worker was built beside pkg/maintain, duplicating the janitor/waterline/cronscheduler loops as workers, with maintain left untouched until the final cleanup chunk deleted pkg/maintain, the maintenance DDL, and the duty metrics in one pass.

**Consequences.** The migration was a switch-over, never a half-converted hybrid: at every intermediate point one complete mechanism was live. The cost was temporary duplication of the loops and a large final cleanup (18 labs swapped or reshaped when maintain died).
