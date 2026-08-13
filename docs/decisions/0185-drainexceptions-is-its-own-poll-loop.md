---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0185 — DrainExceptions runs as its own poll loop, separate from CursorClaim

**Context.** Recorded exceptions need to be retried, and fresh ranges need to
keep being claimed; a single loop doing both would let either kind of work
starve the other.

**Decision.** `DrainExceptions` is a fourth goroutine in `Consume` (cursor
path only) with its own poll loop, independent of `CursorClaim`'s: it claims
recorded exceptions via `ClaimExceptions` and resolves them via
`RecordExceptionSuccess`/`RecordExceptionFailure`.

**Consequences.** A backed-off exception cannot block fresh ranges, and a
stuck range cannot block a resolvable exception — the two loops make progress
independently, converging only at the waterline's blocker terms.
