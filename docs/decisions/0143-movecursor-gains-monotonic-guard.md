---
status: accepted
date: 2026-06-26
phase: "6.5a"
---

# 0143 — `MoveCursor` gains the monotonic guard `WHERE committed < $1`

**Context.** With the waterline (`committed`) speaking for every resolved offset, a backward move would falsely un-resolve offsets. Earlier the guard was deliberately deferred because a single ascending writer could not move the cursor backward.

**Decision.** `MoveCursor` becomes `UPDATE cursor SET committed = $1 WHERE committed < $1` — the `GREATEST`-equivalent guard, enforced in SQL.

**Consequences.** The waterline can never regress regardless of caller ordering. Cost: `RowsAffected()==0` is now ambiguous — either an unregistered group or a monotonic no-op (re-commit of an already-passed offset) — yet the error still reads "no cursor registered". Harmless in the ordered single-worker happy path, where every `message.Id > committed`; to revisit when concurrent within-group advances arrive with the lease work.
