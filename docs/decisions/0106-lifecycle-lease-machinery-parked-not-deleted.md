---
status: accepted
date: 2026-06-23
phase: "4"
---

# 0106 — Dropped lifecycle and lease machinery is parked as reference, not deleted

**Context.** The move to the append-only `message_log` plus `cursor` dropped the per-row `status`/`attempts`/`lease_*` columns, leaving the V1 `ClaimMessages`, `backoff`, `ErrLeaseLost`, and the commented `Record*` blocks referencing columns that no longer exist.

**Decision.** Keep that code parked rather than deleting it: the old lifecycle migrations move to `migrations/old/`, and the V1 claim, `backoff`, and `Record*` code stays in the tree as reference for when leases return. `backoff` showing as an unused-function lint is intentional.

**Consequences.** The tree carries dead code and a known lint on purpose, in exchange for not re-deriving the lease and backoff shapes when they come back. The parked code is not live — it references dropped columns — and is to be deleted or revived when leases land; `backoff` in particular returns with the exception-handling work.
