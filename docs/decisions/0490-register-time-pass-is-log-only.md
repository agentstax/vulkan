---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0490 — The register-time evaluator pass is log-only and never writes to the alerts topic

**Context.** Producer and consumer registration is the first moment a check condition can be evaluated for a new topic or group, which is useful feedback — but registration is not the alert pipeline, and alert state has exactly one write door.

**Decision.** Producer and consumer carry `evaluators []alert.Evaluator`, and `Register` runs them as a log-only pass: findings are logged, never recorded to `__system.alerts`. The pass derives its thresholds live, so a healthy system registers silently.

**Consequences.** Immediate feedback at register time on an unhealthy setup, with zero interference in alert state — `AlertController.Record` inside the executors stays the only writer, so register can never race or contradict the scheduled evaluation.
