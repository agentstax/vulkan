---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0461 — Scheduled work is built on the existing messaging machinery, with one compacted `__system.job_requests` topic

**Context.** The system needed k8s-CronJob-shaped scheduled work. The messaging stack — topics, compaction, key leases, consumer groups, delivery_log — already existed and already solved delivery, retry, and overlap.

**Decision.** A `cron_job` row is the spec: schedule, concurrency, timeout, opaque data, exactly one owner. The `cronscheduler` worker polls the due predicate once a minute and produces one `JobRequest` message per due job onto a single compacted `__system.job_requests` topic, compacted per job so `compaction_head` always marks the current request for each job. Consumers of scheduled work are ordinary consumer groups on that topic.

**Consequences.** Scheduling inherits retention, compaction, per-group delivery, and retry with no new delivery mechanism; supersession of stale requests falls out of compaction for free. Accepted costs: a 1-minute scheduling floor, and drop-don't-backfill semantics — this is deliberately not a backfill engine.
