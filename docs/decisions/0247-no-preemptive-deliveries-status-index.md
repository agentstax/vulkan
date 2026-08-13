---
status: accepted
date: 2026-07-10
phase: "8b"
---

# 0247 — No deliveries.status index until real evidence demands one

**Context.** Keeping `deliveries` as one shared table (0246) raises a
throughput concern at scale, and a `status` index was considered for the
reads that concern implies.

**Decision.** The index was deliberately not added preemptively. `status` is
touched on nearly every write, so today's writes likely already get
Postgres's HOT-update fast path — an index on `status` would end that fast
path for every topic's every state transition, to speed up a read that is
only expensive in an already-contained case. Filed to revisit
with real evidence instead of speculatively.

**Consequences.** Write-path performance is protected by default; the read
stays unindexed until measurement shows it matters. Anyone tempted to add
the index later must weigh it against losing HOT updates on the hottest
write path in the table.
