---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0430 — NoInstanceTarget (-1) for self-claimed consumer loops

**Context.** Consumer-type loops register their own instances through a self-claim door rather than being spawned by the manager, but they still need worker rows for visibility and suspension.

**Decision.** Their worker rows carry `NoInstanceTarget` (-1): the row is visibility plus a suspend toggle, not managed capacity, and a self-claim against it never declines for being over target.

**Consequences.** One `target_instances` column serves both managed workers (a count the manager reconciles toward) and self-claimed loops (a sentinel meaning "not capacity-managed"), so suspension stays uniform — 0 suspends either kind — without the manager trying to scale loops it does not own.
