---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0248 — Two routing_key slices sharing one topic share that topic's drop floor, by design

**Context.** Splitting `message_log` into per-topic tables re-scoped the
retention drop floor from system-wide to per-topic. Within a topic, the floor
is still one `MIN(committed)` across that topic's cursors, regardless of
which `routing_key` slice each group reads.

**Decision.** Left unfixed deliberately: a lagging group reading only
`orders.us.*` still blocks a partition drop that `orders.eu.*` — same topic,
different slice — would otherwise be free to have. Retention and
partitioning are topic-scoped, not per-consumer or per-`routing_key`-slice.

**Consequences.** The cross-contamination is contained, not eliminated. If
two slices in one topic diverge badly enough in consumer lag for this to
matter, the deliberate operational escape hatch is splitting them into
separate topics — which the per-topic split enables manually rather than
automates. This exact behavior is demonstrated in `topiclab`.
