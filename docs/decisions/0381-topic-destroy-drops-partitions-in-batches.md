---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0381 — topic.Destroy drops partitions in batches across multiple transactions

**Context.** `DeleteTopic` dropped `message_log_<id>`/`delivery_<id>`/`delivery_log_<id>` each in one statement inside a single transaction. Every partition pins several shared lock-table slots, so a topic with enough partitions exhausts Postgres's shared lock table: a 2100-partition topic reliably fails with `out of shared memory` (SQLSTATE 53200) on a stock 6400-slot server.

**Decision.** Partition removal moved into a table-agnostic drain in its own file, `pkg/topic/drain.go` (following the `queue.go`/`batch.go` single-concern split). `drainPartitions(ctx, parentTableName)` early-exits when the catalog count is zero, then loops: `listPartitions` reads up to 100 attached partitions per pass from `pg_inherits` (via `to_regclass`, so a missing parent yields zero rows instead of an error), and `dropPartitionBatch` drops that batch in one transaction, until the catalog comes back empty. `deleteTopic` calls the drain, then the original single final transaction deletes the topic row, the topic_id-scoped rows, the now-empty parents, and the plain tables.

**Consequences.** Crash resumability falls out of the ordering: the topic row survives until the final transaction, so a destroy that dies mid-drain is finished by calling `Destroy` again; each pass re-reads the catalog, making the whole drain idempotent, and `IF EXISTS` tolerates a live retention janitor dropping an expired partition between the catalog read and the batch. The old single-transaction shape won the lock race with live producers atomically; batching traded that away (see the bounded-loop backstop). Live-verified: the batched `Destroy` cleared the same 2100-partition topic in ~590ms leaving zero relations and no topic row.
