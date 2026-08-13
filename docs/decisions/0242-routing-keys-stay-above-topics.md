---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0242 — routing_key/bindings stay a coarser concept above topics, not folded into them

**Context.** Once each topic became its own physical log, the obvious further
step was collapsing `routing_key` into topic identity — closer to how a
Kafka consumer subscribes by topic name-pattern.

**Decision.** `routing_key` and `binding` remain a layer above topics, with
their matching logic completely unchanged; `binding` simply gained a
`topic_id` column so bindings are scoped within a topic.

**Consequences.** Routing stays free: a producer can invent a `routing_key`
with zero ceremony, and retroactive binding application keeps working — it
is only possible because the log a message lives in is shared across every
group reading it.
**Rejected:** collapsing `routing_key` into topic identity — a topic carries
real weight (its own sequence, partition set, retention config) that should
not spin into existence from a producer's routing-key typo, and the collapse
would throw away retroactive binding application.
