---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0464 — Missed scheduled times are dropped: the scheduler walks to the newest due time and produces only that

**Context.** After scheduler downtime, several scheduled times for one job may be due at once. The scheduler had to choose between producing every missed time or only the latest.

**Decision.** The `cronscheduler` walks the schedule forward to the newest due time (DB-clock arithmetic against `next_scheduled_time`) and produces exactly one request for it. Missed scheduled times are never produced late. The polling floor is one minute, and schedule rates are validated to be at least one minute apart.

**Consequences.** Downtime of any length yields at most one request per job on recovery — the right default for "run the report" work. This blocks any use of `cron_job` as a backfill engine; backfill needs its own mechanism if ever wanted. **Rejected:** producing every missed time — on a compacted topic the stale burst would supersede itself instantly anyway.
