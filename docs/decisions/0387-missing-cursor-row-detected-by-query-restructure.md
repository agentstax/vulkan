---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0387 — A missing cursor row errors loudly, detected by restructuring the claim query instead of adding an existence check

**Context.** The cursor-advance UPDATE returned zero rows both when the group was caught up (`claimed >= MAX(id)`, which also covers the empty-log NULL case) and when no cursor row existed for (group, topic). Both collapsed into `pgx.ErrNoRows` → `nil, nil` → "caught up," so a consumer whose `Register` was never called would poll forever looking healthy while messages accumulated — silent success, the worst failure mode. Reachable by calling `Consume` without `Register` or by manual cursor-row deletion (not via `topic.Destroy`, which fails loudly on `42P01`).

**Decision.** Restructure the one query rather than add an existence check: the UPDATE moved into a CTE and the result is `SELECT ... FROM old_values LEFT JOIN updated ON true`, so a live cursor always yields a row — NULL low/high means caught up (`CursorRange` fields became pointers to carry this) — and zero rows now unambiguously means the cursor row is missing, returning a permanent error naming the fix ("was Register called?"; a plain error which `pkg/retry` classifies permanent, so no spin).

**Consequences.** **Rejected:** a second existence query — it would double the idle tick, the steady-state hot path. The latency concern was measured, not argued: the old shape paid the `MAX(id)` head probe twice (two scalar-subquery InitPlans, a per-partition MergeAppend probe each); the restructure hoists it into one `head` CTE — parity at 10 partitions, ~25-30% faster at 500 partitions (claiming avg 906µs → 622µs, p99 1.03ms → 736µs), a win that grows with partition count. Complements the register-precondition front door rather than replacing it: this is the datastore backstop that holds even against manual deletion.
