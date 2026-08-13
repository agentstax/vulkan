---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0208 — FanOut keeps its full-table rescan; a per-group high-water mark is deferred

**Context.** `FanOut` re-scans `message_log` without a per-group high-water
mark, re-considering rows it has already materialized `deliveries` rows for.
Adding the routing predicate to its `SELECT` put that inefficiency back in
view.

**Decision.** Leave it. This change only adds the routing predicate to
whatever `FanOut` already did; giving the LIFECYCLE path its own cursor is a
bigger scope decision than a routing change should smuggle in. The limitation
is filed as known and separate follow-up work.

**Consequences.** `FanOut` cost still grows with total log size until the
cursor work is done deliberately; the limitation is documented rather than
silent, and the routing predicate works identically whenever that later
change lands.
