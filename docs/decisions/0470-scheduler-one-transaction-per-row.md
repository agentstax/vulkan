---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0470 — The scheduler produces each due job in its own transaction

**Context.** A scheduler tick may find many due `cron_job` rows. Producing them all inside one shared tick transaction couples every job's fate to every other's and holds locks for the length of the tick.

**Decision.** The `cronscheduler` processes one row per transaction: recheck the due predicate, produce the request, and advance `next_scheduled_time`, then commit before touching the next row. A row whose produce fails is logged at WARN and skipped, and a duplicate produce (idempotency-key hit) is surfaced as a WARN via `ProduceResult` rather than an error.

**Consequences.** A poisoned row cannot roll back or stall its siblings' produces — the failure blast radius is one job. Duplicate detection is observable without failing the tick. **Rejected:** one shared transaction per tick — a single bad row would abort every job's produce and back off the whole scheduler.
