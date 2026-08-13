---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0462 — The job name is the routing key; there is no handler column

**Context.** Consumers of `__system.job_requests` need to receive only the jobs they handle. The binding table already maps consumer groups to routing keys, with exact and wildcard matching.

**Decision.** Consumers bind job names directly as routing keys on `__system.job_requests`. The `cron_job` row carries no handler column, and no handler segment or synthesized key prefix is added to the message key.

**Consequences.** One mechanism owns dispatch; adding a consumer for a job is a normal `Bind`, with wildcard binding available for free. **Rejected:** a handler column or synthesized key prefix — it re-invents the grouping the binding table already owns.
