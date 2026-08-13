---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0398 — No DELETE CASCADEs and no triggers; integrity and logging stay in visible DML

**Context.** Foreign-key CASCADEs and triggers were evaluated twice — once on cost, once purely on maintainability — as a way to simplify `deleteTopic`'s cleanup and the consumer datastore's ~5x-duplicated `logSql` with its per-site `disableDeliveryLog` branches.

**Decision.** No CASCADEs, no triggers. CASCADEs' only code win is `deleteTopic`'s four-statement shared-table loop (the per-topic tables are `DROP TABLE`s — FKs cascade rows, not tables), while the cost lands on hot paths: an FK from `delivery_<id>.message_id` into the message log turns O(1) partition drops into per-row cascaded DELETEs, and even topic_id FKs make every lease insert share-lock the same topic row (multixact churn on a hot referenced row to protect a cold path). Deeper than cost, FKs are wrong for this model: retention deliberately creates orphans (`AllowDropPastCommitted`, dead DLQ rows outliving their partition) that `dropPartition` already cleans set-based in one transaction — integrity constraints would reject states the design allows.

**Consequences.** The two real debts stay, guarded in code: `deleteTopic`'s table list can drift (covered by deletetopiclab's clean-teardown assertions), and `logSql` stays duplicated — if it ever bites, the remedy is a shared Go helper, not a trigger. **Rejected:** trigger-based delivery logging — it relocates the state machine's which-transitions-log policy into PL/pgSQL WHEN clauses, and `DisableDeliveryLog` becomes per-topic trigger existence, so identical code behaves differently per topic based on live schema a review can't read. **Rejected:** a trigger for the `latest_key` upsert — it does identical per-row work to the existing in-statement CTE (one round trip, batcher-pipelined, monotonic `latest_id < EXCLUDED.latest_id` guard visible in code) while hiding it. Lock-in tiebreaker: plain DML/CTEs are the shallow end of Postgres commitment; triggers/PL/pgSQL and FK-on-partitioned-table semantics are the deep end and would collide with moving migrations into code, with the Datastore-interfaces question still open.
