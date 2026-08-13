---
status: accepted
date: 2026-06-20
phase: "3.5"
---

# 0085 — synchronous_commit=local is not measured

**Context.** `synchronous_commit` has five levels; besides `on` and `off` there is `local`, which relaxes only the wait for a synchronous standby, not the local WAL flush.

**Decision.** Do not measure `local`. On a single node with no synchronous standby, `on`, `local`, `remote_write`, and `remote_apply` all behave identically as "wait for local flush"; only `off` skips the fsync, so `local` would measure as `on`.

**Consequences.** A real `local` measurement would require standing up a streaming replica, which is out of scope for a measure-only phase. If replication is ever added, `local` becomes a distinct point worth measuring.
