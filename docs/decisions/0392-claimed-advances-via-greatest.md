---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0392 — cursor claimed advances via GREATEST so a group running both paths cannot regress its frontier

**Context.** With LIFECYCLE groups now holding cursor rows, a group mistakenly running both the cursor-claim path and the FanOut path would have two writers to the same `claimed` column, and a plain assignment from the slower path could move the frontier backward into territory the other path already claimed — reopening ranges for overlapping delivery.

**Decision.** `claimed` advances via `GREATEST`, so no write can regress it below its current value regardless of which path issued the write.

**Consequences.** Misconfiguration (both paths active on one group) degrades to redundant work instead of double-delivery overlap. The invariant is monotonic: the claim frontier only moves forward.
