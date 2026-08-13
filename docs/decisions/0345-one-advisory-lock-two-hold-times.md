---
status: accepted
date: 2026-07-20
phase: "11.5"
---

# 0345 — One advisory lock id, two hold-times: session-scoped for a migrate run, xact-scoped for RegisterSystem

**Context.** Concurrent schema writers must serialize on `common.AdvisoryLock = 0x56554C4B`, but the two writers hold it for very different shapes of work: `RegisterSystem` is one short transaction, while a migrate `Run` walks multiple per-step transactions (each step commits its DDL + version row atomically). Dated approximately; built across July 2026.

**Decision.** `Run` takes a SESSION-scoped `pg_advisory_lock` on a pinned connection, held across all the steps' transactions and released explicitly (or on connection death). `RegisterSystem` uses the xact-scoped `pg_advisory_xact_lock`, which auto-releases at commit/rollback. `IsLocked` queries `pg_locks` as a pre-flight snapshot only.

**Consequences.** Swap the two and both failure modes appear: a session lock on `RegisterSystem` must be manually released (leak risk on an error path), and a xact lock on `Run` would release at the first step's commit, letting a concurrent migrate interleave between steps — exactly what the lock exists to prevent. `IsLocked` accepts a TOCTOU race with a real `AcquireLock` by design; it exists only to turn "blocks forever, silently" into "fails fast with a clear message" for the CLI, not to guarantee anything.
