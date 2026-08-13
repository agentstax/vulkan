---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0261 — Compacted topics resolve latest-per-key at claim time, not by background deletion

**Context.** Kafka's compacted topics keep the latest event per key by appending every write and deleting superseded records in a background pass once a segment ages out. That background pass makes a retention floor a correctness requirement — delete too early and a consumer can miss a key's latest value — and its lag means a new consumer has no guarantee of seeing the latest until compaction catches up.

**Decision.** The log stays append-only — a write is always a new `id` in `message_log_<topic_id>`, never a mutation — and duplicates are resolved at read time: `readMessages` (CURSOR) and `FanOut` (LIFECYCLE) return only the row that is currently the latest for its `compaction_key`. Superseded rows remain physically present but are never selected again.

**Consequences.** Nothing is ever deleted for correctness, so nothing can be deleted too early; retention downgrades to an optional, whenever-convenient disk-space cleanup, fully decoupled from what a claim may return. Latest-per-key holds the moment the producer's transaction commits, so a brand-new consumer group gets it on its very first claim. Cost: superseded rows occupy disk until retention reaps them. **Rejected:** background deletion — reintroduces a correctness-gating retention floor and a lag window with no latest-value guarantee.
