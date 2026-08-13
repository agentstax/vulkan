---
status: superseded
date: 2026-07-11
phase: "8c"
---

# 0264 — The compaction_key index is partial, covering only keyed rows

**Context.** The correlated latest-per-key scan needed a cheap per-partition lookup on `(compaction_key, id)` in each `message_log_<topic_id>` table, but compaction is not the standard consumer setup — most rows carry `compaction_key = NULL`.

**Decision.** The index added in `createTopicLog` is partial — `(compaction_key, id) WHERE compaction_key IS NOT NULL` — rather than covering every row, so unkeyed traffic pays no index-maintenance write overhead for a feature it does not use.

**Consequences.** Keyed lookups stay cheap; unkeyed publishes are untouched. Superseded later in the same phase: once the `latest_key` lookup replaced the correlated scan in the read path, no production query consumed this index and it was dropped outright (see the decision to drop the compaction_key partial index).
