---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0186 — MessageException and MessageTerminal are two distinct types, not a flag

**Context.** `Commit` needs to distinguish a retryable failure (goes to
`ready` with an attempt budget) from an unrecoverable one like a bad payload
(goes straight to `dead`).

**Decision.** Two distinct types, `MessageException` and `MessageTerminal`,
passed as separate slices to `Commit` — not one type with a bool flag or a
sentinel error. This mirrors the codebase's existing
`RecordTerminal`/`RecordFailure` split on `LifecycleClaim` rather than
inventing a new mechanism.

**Consequences.** No boolean blindness: a call site cannot silently mean the
wrong thing, because the retryable/terminal distinction is in the type, not
in a flag whose meaning needs external context.
