---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# Append-only is for history tables

## Context

The roadmap asked whether making all tables append-only (Cassandra-like)
would buy audit/debuggability everywhere and partition-based drops. The
framing has a category error: Cassandra is append-only at the storage
engine (LSM + compaction) while its data model still presents mutable
rows. Postgres already versions every row on UPDATE (MVCC, vacuum as
compactor); emulating append-only in the data model means hand-building
compaction as user-space DELETE sweeps — themselves MVCC churn — while
giving up HOT updates. The experiment for truth-in-append-rows already
ran: 0519 was built and rolled back; 0570 settled the opposite shape.

## Decision

Append-only is the shape for history tables — one row per event, the
[0511]/[0570] log-companion pattern: a mutable truth row plus a
same-transaction append-only log. Tables holding coordination state,
derived caches, or dispatch indexes stay update-in-place. Blanket
append-only is rejected.

The mutable remainder, classified:

- Coordination state: lease, key_lease, worker_instance (heartbeat),
  cursor. History is lock churn; the reclaim worth keeping is already a
  delivery_log 'expired' row. Newest-row-per-key reads would sit on the
  claim path.
- Derived caches: compaction_head. Its history IS message_log; an
  append-only cache is a second derivation of a fact the source owns.
- Dispatch index: delivery_<id>. Its history is delivery_log's job;
  event-sourcing the index makes every claim a DISTINCT ON aggregation
  and duplicates the mechanism.
- Catalog truth rows: topic, binding, worker, cron_job, consumer_group,
  system — settled by 0570's shape.

The audit goal is already delivered selectively: message_log,
delivery_log, topic_log, binding_log, migration_log, worker_log
(planned), worker_run_log (parked), and the job_requests topic as
cron's firing history.

## Consequences

- No table changes shape; the rule closes the roadmap item.
- The one surviving nugget is orthogonal to append-only: delivery_<id>
  and delivery_log_<id> are message_id-keyed and cleaned by range
  DELETEs at partition drop; partitioning both BY RANGE (message_id) on
  message_log's bounds would turn those into drops. Parked
  (benchmark-gated; create-ahead must ride the producer's self-heal,
  and the hot claim table takes on planner overhead).
