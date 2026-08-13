---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0245 — Topics are registered explicitly and never auto-created on use

**Context.** A topic is a durable resource commitment: registering one
constructs a physical `message_log_<topic_id>` table and locks in
configuration (sequence, partition set, retention settings), some of it
immutable.

**Decision.** Topics exist only via explicit `topic.Register`. Producing to
or claiming from an unregistered topic id fails with a clean Postgres
`42P01 undefined_table` error — it never silently creates the topic. This is
proven end to end in `topiclab`.

**Consequences.** Creating a topic is a deliberate moment, not an incidental
side effect of a produce or claim call — a typo cannot fork a whole new
physical log into existence. This is the same weight argument that kept
`routing_key` (free text, zero ceremony) as a separate concept above topics
(0242). Partitions, by contrast, keep self-creating via
`EnsureNextPartition`: their names are computed from `id / partitionSize`,
never caller-supplied, so no typo can fork one.
