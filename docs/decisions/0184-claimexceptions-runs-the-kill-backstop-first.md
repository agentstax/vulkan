---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0184 — ClaimExceptions dead-letters crash-looping rows before claiming

**Context.** An exception whose processing crashes the worker never reaches
`RecordExceptionFailure`, so it can never resolve itself — without a backstop
it would be re-claimed on expiry forever.

**Decision.** `ClaimExceptions` runs a kill backstop first: expired-`inflight`
rows already at `maxAttempts` are moved to `dead` with no user code involved.
Only then does it claim `ready` and expired-`inflight` rows into `inflight`,
joined to `message_log` for the payload.

**Consequences.** The backstop is the only way a poison exception ever leaves
the retry loop — placing it inside the claim query means no separate reaper
process is needed, and a crash-looping message terminates at `dead` after its
attempt budget exactly like an ordinary exhausted failure.
