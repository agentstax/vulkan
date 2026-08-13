---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0246 — deliveries stays one shared table across every topic, unlike message_log

**Context.** With `message_log` split into per-topic physical tables, the
symmetric move would be per-topic `deliveries` tables too.

**Decision.** `deliveries` remains a single shared table; topic scoping is a
plain `topic_id` column plus the wider composite PK
`(consumer_group, topic_id, message_id)`.

**Consequences.** `topic.Register`/`Destroy` stay cheap — they create and
drop only the topic's own log tables, not N companion tables per topic.
**Rejected:** per-topic `deliveries` tables — `message_log` needs physical
separation for two structural reasons, its own `BIGSERIAL` sequence and
retention by `DROP TABLE` of topic-owned partition sets, and `deliveries`
has neither: rows are ephemeral (deleted or resolved continuously, no
retention-drop mechanism) and are not keyed by a shared sequence, so the
split would add real DDL-lifecycle cost for no matching benefit.
