---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0181 — A deliveries row exists only while a message needs individual attention; success is no row

**Context.** On the cursor path a failing message used to drag its whole
range down, and the happy path's zero-rows-per-success property ruled out
materializing a row per message just to track the rare failure.

**Decision.** `Commit` takes two slices — `MessageException` (retryable) and
`MessageTerminal` (unrecoverable) — and records only those: one sparse
`deliveries` row per failing message, exceptions inserted as `ready`,
terminals as `dead`. Status collapses to `ready | inflight | dead` with no
`done`: a happy-path success writes nothing, and when a recorded exception
later succeeds, `RecordExceptionSuccess` deletes its row rather than flipping
a status. The range's lease always frees regardless of individual outcomes —
one bad message no longer fails its batch-mates.

**Consequences.** Row existence is itself the "still needs attention" signal;
resolved exceptions leave no trace in `deliveries`, and only `dead` rows
persist, as the dead-letter record. The row is the handoff between the
range-claim happy path and the per-row retry state machine — it exists
exactly as long as a message needs individual treatment.
