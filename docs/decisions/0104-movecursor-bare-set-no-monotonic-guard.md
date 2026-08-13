---
status: superseded
date: 2026-06-23
phase: "4"
---

# 0104 — `MoveCursor` stays a bare `SET position = $1` with no monotonic guard

**Context.** A cursor must never move backward. A `GREATEST(position, $1)` guard would enforce that in SQL, but it also changes what `RowsAffected()==0` means.

**Decision.** Leave `MoveCursor` as a bare `UPDATE cursor SET position = $1`. With an ordered claim and one consumer per group, advances are strictly ascending, so monotonicity holds without a guard — and the bare `UPDATE` keeps the `RowsAffected()==0` check meaning "unregistered group".

**Consequences.** Safe only under the one-consumer-per-group model; the guard becomes necessary once concurrent advances hit a shared cursor. Superseded when the claim-from-log work replaced `position` with the `committed` waterline and `MoveCursor` gained the monotonic guard `WHERE committed < $1`.
