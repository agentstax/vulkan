---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0268 — latest_key is one shared table across all topics, not per-topic

**Context.** Message logs are per-topic physical tables (`message_log_<topic_id>`) because they scale with message volume. `latest_key` holds exactly one row per (topic, key), so it scales with distinct-key count instead.

**Decision.** `latest_key(topic_id, compaction_key, latest_id)` is a single table shared across every topic, created by migration `006_latest_key` — matching the shape of `cursor`/`deliveries`/`binding`, which are likewise shared and volume-independent, rather than `message_log`'s per-topic shape.

**Consequences.** No dynamic DDL on topic creation for this table; size stays bounded by live distinct keys regardless of republish count. Topic teardown must clean its `latest_key` rows like the other shared tables (the pre-existing `DeleteTopic` cleanup gap applies here identically — same gap, one more table).
