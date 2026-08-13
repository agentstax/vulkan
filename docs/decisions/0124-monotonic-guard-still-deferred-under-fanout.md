---
status: superseded
date: 2026-06-23
phase: "5"
---

# 0124 — Fan-out does not need a monotonic cursor guard; `MoveCursor` stays `SET position = $1`

**Context.** An earlier forecast said a `GREATEST(position, $1)` guard would be needed "once fan-out puts concurrent advances on a shared cursor." That forecast was wrong about where the risk lives: fan-out is different groups on different cursor rows, one consumer each.

**Decision.** Keep `MoveCursor` as the bare `SET position = $1`. Advances on any one cursor are still strictly ascending under one-consumer-per-group, so monotonicity holds without a guard. Concurrent advances on a shared cursor only appear when multiple workers compete within a group.

**Consequences.** The guard is deferred to the within-group claim-from-log work, where it actually belongs — and it landed there, superseding this: `MoveCursor` became `committed = $1 WHERE committed < $1`. Until then, `RowsAffected()==0` keeps its single meaning of "unregistered group".
