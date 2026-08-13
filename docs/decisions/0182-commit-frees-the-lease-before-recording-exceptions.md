---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0182 — Commit frees the lease before recording exception rows

**Context.** `Commit(group, token, []MessageException, []MessageTerminal)`
both gives up range ownership and inserts failure rows. The inserts are plain
INSERTs with no ownership check of their own, so the ordering decides whether
a stale worker can write rows for a range it no longer owns.

**Decision.** `Commit` runs the token-guarded `DELETE FROM lease` first; zero
rows matched means the lease was reclaimed — return `ErrLeaseLost` and bail
before touching `deliveries` at all. Only on a match does it insert exceptions
as `ready` and terminals as `dead`, in the same transaction.

**Consequences.** The token-guarded DELETE is simultaneously the ownership
check and the give-up — there is no window between check and action for a
race to land in. A worker whose lease was reclaimed (its range now owned and
re-processed under a rotated token) cannot inject stale failure rows. The
same `ErrLeaseLost` guard shape is reused by `RecordExceptionSuccess` and
`RecordExceptionFailure`.
