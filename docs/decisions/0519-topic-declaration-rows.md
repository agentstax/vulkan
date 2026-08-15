---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0519 — Topic config lives in append-only declaration rows; topic keeps identity only

**Context.** [0518] makes code the only config writer and the latest write the
truth, which trades away the mismatch error's conflict detection: two apps
declaring one topic differently, or an operator's raw SQL being overwritten at
the next boot, leaves no record. The topic table has no history at all -- a
config change is an UPDATE and a fresh updated_at. [0511] already established
the shape this needs: a stable entity row plus append-only declaration rows,
newest by MAX(id).

**Decision.**
- topic keeps identity only -- id, system_id, schema_version, created_at. Its
  id is referenced by consumer_group, worker, cron_job and migration_log,
  keyed by compaction_head, and names message_log_<id>,
  message_log_<id>_<n> and idempotency_key_<id>, so it can never move.
- topic_declaration holds name, partition_size and the four mutable config
  fields, plus declared_by (common.ProcessIdentity) and declared_at. The newest
  row per topic_id is effective, by MAX(id). No currency marker: pure
  append-only, matching binding_declaration.
- Identity is what a topic is looked up by. schema_version is half the lookup
  key, so it stays on topic; partition_size is immutable but nothing resolves
  a topic by it, so it is a declaration field. Immutability and identity are
  different properties.
- Rows are full snapshots, not deltas, so the config at any instant reads
  without replay. A declaration is appended only when a field differs from the
  newest row -- every row is a real change, and a restart with unchanged
  config writes nothing.
- partition_size's immutability is enforced at append: compare against the
  newest row and reject a difference. That is [0518]'s narrowed
  ErrTopicConfigMismatch, and this is its home.
- Uniqueness of (name, schema_version) becomes procedural. registerTopic
  already holds pg_advisory_xact_lock(hashtext('topic:' || name)) and
  re-checks under it; renameTopic gains the same lock and check, and
  ErrTopicNameTaken comes from that check rather than from 23505. A rename
  holds the old and new keys in sorted order so opposite concurrent renames
  cannot deadlock.
- Rename stays family-scoped: one appended declaration per schema_version
  sharing the name, in one transaction. Physical tables never move.

**Consequences.** Config history becomes queryable -- when a value changed and
which process declared it -- which is how two apps fighting over one topic gets
found. The database no longer prevents a duplicate live name; register already
depended on the advisory lock for that invariant, so rename joins it rather
than opening a new exposure. GetTopic resolves through the newest declaration,
at Register and janitor frequency, never per message. Rejected: superseded_at
or is_current carrying a partial unique index, which keeps uniqueness
declarative but writes the prior row, so the table stops being append-only;
and keeping name on topic as mutable current state with declarations for audit
only, which preserves the 23505 path and leaves rename untouched but splits one
fact between a current-value home and its history. Worker metadata takes the
same shape later, not here.
