---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0487 — The alert domain owns measurement and decision; producer and consumer inject it

**Context.** Both the producer and consumer sides need the check conditions (for the register-time pass and the executors). The first build followed the "write it on each side's datastore" convention and copied the queries, thresholds, and texts into each side — two drifting copies of one domain's logic.

**Decision.** The condition lives once, as the `Evaluate` controller in each check's package (`pkg/alert/partitioncount`, `pkg/alert/compactionreadcost`), and producer/consumer inject it as `evaluators []alert.Evaluator`. The per-side copies were deleted.

**Consequences.** One home per condition — a threshold or query change happens once. Clarified rule: the duplication convention covers shared *needs* each side implements in its own style, not another domain's measurement-and-decision logic. **Rejected:** per-side copies of the check queries and thresholds — that convention was misapplied here.
