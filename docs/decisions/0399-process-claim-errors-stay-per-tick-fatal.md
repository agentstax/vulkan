---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0399 — Process keeps per-tick-fatal claim errors; no loop-level retry/backoff

**Context.** Open question: when a claim errors inside `Process`'s poll loop, should the loop itself retry with backoff, or is "the caller of Consume restarts the process" the intended boundary?

**Decision.** Keep per-tick-fatal. An unbounded loop-level retry would mask a genuinely permanent error forever — a crash is both a signal and a chance to fix things on restart — and it would make `Process` a special case out of step with `RollWaterline`, `DrainExceptions`, `Project`, and the janitor, which all share the identical give-up-and-exit shape via `DatastoreRetry`'s bounded MaxRetries budget.

**Consequences.** Transient blips are absorbed by the bounded `DatastoreRetry` budget inside each datastore call; anything that exhausts it exits the loop and surfaces to the caller. Uniform failure shape across every long-running worker loop.
