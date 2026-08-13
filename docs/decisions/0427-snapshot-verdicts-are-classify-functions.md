---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0427 — Snapshot verdicts are classify functions returning named enums

**Context.** Operators need a health verdict per worker and per cron job, and the codebase's standing rule is that policy tables become a pure classify function returning a named enum, driven by an exhaustive switch.

**Decision.** `WorkerSnapshot` and `CronJobSnapshot` render their verdicts through classify functions (classifyWorker, Overdue): suspended/claimed/unclaimed for workers, with overdue judged against a flat 10-minute threshold for cron jobs. The `cron_job` owner CHECK was tightened to exactly-one so every CronJobSnapshot carries an owner.

**Consequences.** Verdict policy lives in one named function per snapshot type instead of being smeared across queries or callers; adding a verdict means extending the enum and the switch. The flat threshold is a deliberate simplification — no per-job tuning surface exists yet.
