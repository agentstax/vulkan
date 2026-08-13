---
status: accepted
date: 2026-06-15
phase: "2"
---

# 0042 — Failures return to ready with a backoff on can_run_after; max attempts goes dead

**Context.** With the row no longer deleted on claim, a failed message needs
an explicit retry path — and immediate re-eligibility would hammer a failing
handler, while unlimited retries would loop a poison message forever.

**Decision.** When `consumerFunc` returns an error and `attempts` is below the
maximum, `RecordFailure` sets the row back to `ready` with `can_run_after =
now() + backoff(attempts)` and records `last_error`. Once `attempts` reaches
the maximum, the row goes to `dead` — terminal, no more retries.

**Consequences.** The claim predicate must include `can_run_after <= now()`,
so a backed-off row is invisible to claims until its time arrives. Poison
messages self-terminate after bounded attempts, and `last_error` preserves why.
