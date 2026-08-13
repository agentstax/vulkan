---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0188 — Resolution verbs are RecordExceptionSuccess/RecordExceptionFailure, not Ack/Nack

**Context.** The methods that resolve a claimed exception needed names; the
message-queue world's conventional pair is ack/nack.

**Decision.** `RecordExceptionSuccess` (deletes the row, token-guarded) and
`RecordExceptionFailure` (exhausted attempts move the row to `dead`,
otherwise back to `ready` with `backoff(attempts)`, token-guarded) — named
after the codebase's own existing verbs
(`RecordSuccess`/`RecordFailure`/`RecordTerminal`), not borrowed jargon.

**Consequences.** One vocabulary for the same concept across both consume
paths; a reader who knows the lifecycle verbs already knows what these do.
**Rejected:** `Ack`/`Nack` — outside-domain jargon that would coexist with,
and contradict, the established `Record*` naming.
