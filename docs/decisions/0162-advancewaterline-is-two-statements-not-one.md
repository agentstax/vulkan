---
status: accepted
date: 2026-06-30
phase: "6.5b"
---

# 0162 — AdvanceWaterline is two statements, not one UPDATE

**Context.** The obvious implementation is a single `UPDATE cursor SET
committed = LEAST((SELECT MIN(low) FROM lease), claimed)`. A live
multi-worker run showed it is buggy under concurrency; the deterministic lab
missed it because it is single-threaded.

**Decision.** `AdvanceWaterline` runs two statements: one plain `SELECT
LEAST((SELECT MIN(low) FROM lease), claimed)` — one consistent snapshot —
then a separate `UPDATE ... SET committed = GREATEST(committed, $target)`.

The single-statement form races with a claim that advances `claimed` and
inserts its lease in one transaction. Under READ COMMITTED the roller's
UPDATE blocks on the claim's `cursor` row lock; when it proceeds,
EvalPlanQual re-reads the target row (new `claimed`) at its newest version
but runs the `lease` subquery on the statement's original snapshot — new
`claimed` visible, new lease invisible — so `LEAST(NULL, 10) = 10` and
`committed` passes an in-flight range.

**Consequences.** The computed target can only lag reality, never overshoot an
open lease; `GREATEST` keeps `committed` monotonic. Standing rule: every
future blocker term must be read in that same single SELECT alongside
`claimed`, never split across snapshots.
**Rejected:** `FOR UPDATE` on the single-statement form — a not-yet-inserted
lease cannot be locked, and reads never block writers.
**Rejected:** raising the isolation level — converts the race to abort-retry,
worse under contention.
