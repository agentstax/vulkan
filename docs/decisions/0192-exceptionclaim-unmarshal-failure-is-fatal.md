---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0192 — ExceptionClaim payload unmarshal failure is fatal, not retried

**Context.** When `DrainExceptions` claims a recorded exception, the payload
from `message_log` is unmarshaled again before the handler runs. That
unmarshal could in principle fail — should it get its own retry/dead-letter
path?

**Decision.** `ExceptionClaim`'s `json.Unmarshal` failure returns the raw
error, treated as fatal.

**Consequences.** The payload already deserialized successfully once, in
`CursorClaim`, to reach the exception path at all — and `message_log` rows
are immutable — so a failure here can only mean an invariant broke elsewhere.
Surfacing it loudly beats building recovery machinery for an unreachable
case.
**Rejected:** a dedicated retry/dead-letter path for unmarshal errors —
unreachable by construction, and it would hide corruption instead of
reporting it.
