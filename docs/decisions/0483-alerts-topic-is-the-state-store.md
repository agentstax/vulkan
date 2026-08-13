---
status: accepted
date: 2026-08-13
phase: "14a"
---

# 0483 — The `__system.alerts` topic is the alert state store, dedup memory, and integration surface

**Context.** Alert state — which alerts are active, at what severity, since when — needs a durable home, and external systems need somewhere to read alerts from. A dedicated state table would be a second mechanism beside the messaging the system already ships.

**Decision.** Alert state lives entirely on the compacted `__system.alerts` topic: the compaction head per alert key is the current state, comparison against the head is the dedup memory, consumers of the topic are the integration surface, and the repeat-interval republish refreshes the head so retention can never sweep a live alert — the topic is its own retention keepalive.

**Consequences.** No alert-state table, no separate dedup store, no bespoke export path; anything that can consume a topic can consume alerts. The repeat republish is load-bearing, not cosmetic: it is what keeps an active alert's head inside the retention window.
