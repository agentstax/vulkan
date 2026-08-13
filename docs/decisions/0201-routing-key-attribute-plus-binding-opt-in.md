---
status: accepted
date: 2026-07-03
phase: "7"
---

# 0201 — Producers publish a routing_key attribute; groups opt in via binding rows; no binding means receive everything

**Context.** Routing was needed without producers addressing consumers
directly, and without changing behavior for any consumer that existed before
routing did.

**Decision.** `message_log` gains a `routing_key TEXT` column;
`Datastore.AppendMessage`/`WorkProducer.Produce` take a `routingKey string`,
with `""` stored as SQL `NULL` rather than `''`. A new
`binding(consumer_group, pattern, display)` table (indexed on
`consumer_group`) lets a group opt into only the events whose `routing_key`
matches a `pattern`. A group with no binding rows receives everything.
`AppendMessage` never reads `binding` — the producer writes one attribute
and never learns a consumer exists.

**Consequences.** All pre-routing behavior is unchanged by default; filtering
is strictly opt-in per group. The producer/consumer decoupling is total: the
only coupling surface is the `routing_key` value and the `binding` rows that
match it.
