---
status: accepted
date: 2026-07-11
phase: "8c"
---

# 0273 — The synchronous latest_key write cost was measured and accepted, hot-key serialization included

**Context.** Every keyed publish pays a second write — the `latest_key` upsert in the producing transaction. Flagging that cost was not enough; it needed numbers before being accepted.

**Decision.** `latestkeyswritelab` quantified it. Sequential, uncontended publishes: no measurable fixed cost (unkeyed versus keyed within ±10-20% noise, no consistent direction). Fifty concurrent goroutines each on their own key versus all fifty on one key: 2.5-2.9x slower under full serialization on the `ON CONFLICT DO UPDATE` row lock, reproduced across repeated runs. A ~1,000-update hot-key burst left ~500 dead tuples pending autovacuum — real but bounded bloat, since the table holds exactly one row per (topic, key) regardless of republish count. The cost was accepted on those terms.

**Consequences.** A non-issue for the many-distinct-keys workloads this design targets; a known, quantified cost for a workload concentrating writes on one very hot key. The numbers are on record for anyone hitting that pathology later.
