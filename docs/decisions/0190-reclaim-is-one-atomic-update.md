---
status: accepted
date: 2026-07-03
phase: "6.5c"
---

# 0190 — Reclaim is one atomic UPDATE, not DELETE plus INSERT

**Context.** Reclaim was originally a `DELETE` of the expired lease followed
by a fresh `INSERT`. With the new `lease.reclaims` counter, that shape
silently reset the counter to 0 on every reclaim — the exact value the
repeated-reclaim cap depends on.

**Decision.** Reclaim is a single atomic `UPDATE` on the lease row: `reclaims
= reclaims + 1`, token rotated, `until` refreshed.

**Consequences.** The counter survives across reclaims, making
`MaxRangeReclaims` (see 0191) enforceable at all; token rotation — the guard
that lets `CommitRange` from a superseded worker no-op — is preserved
explicitly rather than falling out of a row replacement. Supersedes the
delete-then-insert shape from the original reclaim implementation.
