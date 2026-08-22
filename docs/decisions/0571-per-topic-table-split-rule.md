---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# Per-topic table split rule

## Context

The system-table churn evaluation asked whether compaction_head should be
per-topic, then widened to every shared table. Churn alone doesn't decide:
cursor's hot updates are HOT-eligible in a shared table, lease churn is
per-group, binding is near-static. The deciding lens is logical grouping —
which tables belong to a topic's table family (message_log_<id>,
idempotency_key_<id>, delivery_log_<id>) whose interpolation machinery
already exists.

## Decision

A table splits per-topic when every row has exactly one owning topic
(directly or through its consumer group) and no reader needs the table
before knowing the topic. It stays shared when rows can exist at system
scope with no topic, or when it is the catalog that resolves names to
topic ids.

- Split: cursor, lease, key_lease, compaction_head — their values are
  message_log_<id> ids and compaction-key vocabulary; every reader
  (consumers, advancer, janitor, metrics snapshot) already holds the
  topic id.
- Split: binding and binding_log together — one mechanism
  (declareBindings appends the log row and rewrites binding rows in one
  transaction), identical ownership, no topic-blind reader.
- Keep shared: worker, worker_instance, cron_job, migration_log (rows
  can be system-scoped — no per-topic home — and their primary readers
  are single cross-scope queries: fleet listWorkers, cron due-walk);
  system, topic, topic_log, consumer_group (the catalog; name -> id
  resolution precedes naming any per-topic table, and topic_log is the
  catalog's own history [0570]).

## Consequences

- A topic's family grows 3 -> 9 interpolated tables; the shared schema
  reduces to exactly catalog + fleet + cross-scope history.
- Topic destroy becomes DROP TABLE for all nine; the bulk DELETE ...
  WHERE topic_id waves in topic destroy and janitor sweeps disappear.
- Per-table storage tuning (fillfactor) becomes possible per topic.
- Amends the binding_log retention design ([0511] shape): the pruner
  stays one OwnerSystem worker but runs one batched DELETE per topic's
  table per tick.
- Cursor keeps its ON DELETE CASCADE FK to consumer_group;
  lease/key_lease already have no FKs.
- The compaction_head DDL comment's shared-table rationale (scales with
  distinct-key count) reasoned about size, not update churn, and is
  superseded by this rule.
