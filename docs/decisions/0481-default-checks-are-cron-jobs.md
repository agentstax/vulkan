---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0481 — Each default alert check is its own cron job and worker-kind subpackage

**Context.** The system should watch its own operational cliffs — partition creation falling behind, compaction read cost growing — without introducing a monitoring mechanism separate from what it already ships.

**Decision.** Each check is a `cron_job` producing requests onto `__system.job_requests`, implemented as its own worker-kind subpackage — `pkg/alert/partitioncount` and `pkg/alert/compactionreadcost` — each with definition, declare, and execution over an `Evaluate` controller and a layered datastore. `pkg/alert` root stays pure vocabulary.

**Consequences.** Alerts get scheduling, overlap protection, retry, suspension, and fleet visibility from existing machinery, with zero new mechanisms. Accepted cost: evaluation is level-triggered at the job's schedule — a condition crossed between runs waits for the next tick, and suspending a check job freezes its alert state until unsuspend.
