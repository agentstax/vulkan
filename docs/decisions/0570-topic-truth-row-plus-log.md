---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# 0570 — Topic truth stays on the topic row; history is an append-only log (topic_log, binding_log)

**Context.** [0519] was built: truth in append-only declaration rows, topic as
identity only. The code showed the cost -- newest-row lateral joins in every
reader across five domains, procedural (name, schema_version) uniqueness with
advisory locks on every name write, a lock-plus-re-read on the found-path
append. A single-table variant (per-row id plus a stable topic id in one
append-only table) was evaluated and rejected: a repeating stable id cannot be
a foreign-key target, so consumer_group/worker/cron_job/migration_log lose
their FKs and cascades, while every newest-row read and procedural lock
remains. The house already holds the working shape: binding [0511] -- the
current table is the enforced truth, an append-only trail is written in the
same transaction, and machinery never reads the trail.

**Decision.**
- topic keeps the full current-state row: identity, name, partition_size, the
  mutable config columns, UNIQUE (name, schema_version). Reads stay plain
  column reads; rename stays one UPDATE with ErrTopicNameTaken from 23505;
  register keeps its per-name advisory lock for the create race only.
- topic_log is the append-only history: a full snapshot (name,
  partition_size, config fields, declared_by = common.ProcessIdentity,
  declared_at), one row appended in the SAME transaction as every create,
  config replace, and rename (one row per schema_version on rename).
  Machinery never reads it; it exists for operators to query.
- updated_at stays on the topic row: cheap current-state display, while the
  log carries the full change history.
- `_log` is the established suffix for append-only event history
  (message_log, delivery_log, migration_log -- the last even machinery-read).
  binding_declaration renames to binding_log, its index to binding_log_group,
  and the Go surface follows. Declare* verbs and declared_by/declared_at
  columns stay: declaring is the action, the log records it.
- The parked failure-evidence table renames to worker_run_log; worker_log is
  reserved for worker metadata history when it takes this same shape.

**Consequences.** Supersedes [0519]; its build was rolled back before ever
being committed. Config history stays queryable -- when a value changed and
which process declared it -- without a second read path: the log is
display-only data, the same answer [0511] gave to "one fact, two homes".
Rejected: the single append-only table with a stable id (FK hole, keeps every
rough edge; an anchor-row self-reference restores FKs but adds a subtler
invariant and simplifies nothing); an is_current marker with a partial unique
index (writes the prior row -- [0519]'s own rejection stands).
