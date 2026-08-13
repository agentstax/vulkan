---
status: superseded
date: 2026-07-08
phase: "8a"
---

# 0226 — Retention shipped scoped to the one shared log, per-topic scoping deferred

**Context.** Retention was built on a single shared `message_log`: one
`RetentionTTL`, and a drop floor of `MIN(committed)` across every `cursor`
row regardless of which `routing_key` each group actually consumes.

**Decision.** Ship retention per-log anyway, with the known limitation filed
rather than fixed: one lagging group on an unrelated stream blocks partition
drops for everyone sharing the table. Kafka avoids this because
`retention.ms` is per-topic and each topic is its own log; the filed note
said the system may need an actual topic concept — its own log and partition
set — instead of `routing_key` filtering over one shared table.

**Consequences.** Retention and the floor were system-wide until the topic
concept landed.
**Superseded** by per-topic tables (0241): each topic became its own
`message_log_<topic_id>` and `cursorFloor` gained `WHERE topic_id = $1`,
bounding a lagging group's effect to its own topic.
