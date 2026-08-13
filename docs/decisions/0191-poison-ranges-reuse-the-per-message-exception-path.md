---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0191 — A range past MaxRangeReclaims moves into the per-message exception path

**Context.** A range whose processing crashes the worker gets reclaimed
forever (the known handoff left open when leases landed, see 0165). Somewhere
the loop has to end, and the messages need individual handling — usually one
of them is poison and the rest are fine.

**Decision.** Once a lease's `reclaims` counter passes `MaxRangeReclaims`,
every message in the range is inserted into `deliveries` as a `ready` row
with a fresh attempt budget, and the lease is deleted for good instead of
being handed out again. No range-level dead-letter mechanism was built.

**Consequences.** Each message gets the same per-message
retry/backoff/dead-letter treatment as an ordinary `Commit`-recorded failure,
so `AdvanceWaterline` needed zero new logic — its blocker term was already
generic over any unresolved exception. The range's healthy messages resolve
individually via `ClaimExceptions` + `RecordExceptionSuccess`; only the
actually-poison one ends at `dead`.
**Rejected:** dead-lettering at range granularity — a second failure-handling
mechanism that would condemn a range's healthy messages along with the poison
one.
