---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0241 — Each topic is its own physical table with its own sequence, partitions, and janitor

**Context.** Two filed bugs shared one root cause: with a single shared
`message_log`, the retention drop floor `MIN(committed)` spanned every
`cursor` row, so one lagging group blocked drops for every unrelated stream;
and a planned compaction lookup would have to probe every live partition up
to a claim's high bound, because one shared `BIGSERIAL` diluted how densely
a rarely-used key's own writes cluster. Both are one shared sequence/log
doing the job of many.

**Decision.** Following Kafka's model — a topic *is* its own log, not a
filter over a shared one — each topic gets its own physical table
`message_log_<topic_id>` with its own dense id sequence, its own partition
set (`message_log_<topic_id>_<n>`, via package-level
`logTable(topicID)`/`partitionTable(topicID, n)` helpers, duplicated per
package rather than shared), and its own janitor. The log-shape knobs moved
onto the topic: `topic.Config` gained `RetentionTTL` and
`AllowDropPastCommitted` alongside the existing `PartitionSize`.
`cursorFloor` is scoped `WHERE topic_id = $1`; `FanOut`,
`ClaimMessagesWithLifecycle`, `Bind`, `ClearBindings`, and the producer's
`AppendMessage` all became topic-scoped. The original global `message_log`
DDL and the dead V1 consumer datastore package were deleted outright.

**Consequences.** A lagging group's drop floor and a compaction lookup's
probe cost are now bounded by one topic's own volume, not the whole
system's — both problems fixed as a structural side effect of one change
rather than two separate patches on a shared table. The threading work also
surfaced two latent bugs: `ReclaimWithCursor` and
`FreshClaimMessagesWithCursor` lacked `topic_id` filters, so two consumers
sharing a group name across topics could have cross-contaminated each
other's reclaim and cursor-advance behavior.
