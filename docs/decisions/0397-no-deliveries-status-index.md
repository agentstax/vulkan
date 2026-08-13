---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0397 — No index on deliveries status for v1; reopen only on measured evidence, and prefer a partial index then

**Context.** A status index on the per-topic delivery tables was a standing question. `delivery_<topic_id>` has only its (consumer_group, message_id) primary key, and no UPDATE touches an indexed column, so every state transition keeps the HOT fast path. Status is the single most frequently written column in the table — every recording function sets it.

**Decision.** Confirmed for v1: no index. A status index would end the HOT fast path for every state transition on every topic to speed a read that is only expensive in an already-contained case. The case for the index weakened twice since first written: the blast-radius worry (one lagging topic's bloat degrading other topics' queries) was fixed structurally by the per-topic delivery split, and LIFECYCLE's parking took `ClaimMessagesWithLifecycle` out of live traffic — `ClaimExceptions` is the only status-filtered scan left, and the exception window keeps its table sparse by design.

**Consequences.** During a failure burst, a slow unindexed exception scan is closer to accidental backpressure than a bug: fresh happy-path parks aren't gated by `ClaimExceptions`' cost, and speeding retry-claims against a dependency that is provably down just burns through maxAttempts sooner, permanently dead-lettering messages that would have succeeded on recovery. Reopening condition: real evidence of a hot claim scan (pg_stat_user_tables / EXPLAIN ANALYZE on a lagging group), never speculation — and if added, prefer a partial index (WHERE status IN ('ready', 'inflight')) so terminal done/dead rows drop out of upkeep instead of bloating the index forever.
