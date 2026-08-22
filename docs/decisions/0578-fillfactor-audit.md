---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# 0578 — fillfactor audit: adopt nothing, defaults everywhere

## Context

The ROADMAP fillfactor-audit item asked whether lower fillfactor (page
slack that keeps update chains HOT) improves steady-state throughput,
naming cursor, compaction_head, lease, key_lease — with [0574]'s 36k
dead tuples/15s on one hot compaction_head row as motivating evidence —
and requiring live before/after benchmarks, never reasoning alone.

A static pass classified every table first. HOT needs the update to
avoid indexed columns AND same-page room; fillfactor only buys the
second. Real candidates: cursor, compaction_head, and delivery (not on
the original list — dense-inserted then updated, the classic case).
Ruled out structurally: lease (steady state insert->delete; the reclaim
UPDATE rotates token, a PK column), key_lease (Release deletes the row),
worker_instance (heartbeat bumps the indexed expires_at — non-HOT by
index design, noted for the idle-fleet item), cron_job (indexed
next_scheduled_time, negligible rate).

## Decision

Every table keeps default fillfactor. Benchmarks (bench/fillfactor, plus
bench/compaction's hot-key cells rerun via a new -head-fillfactor flag)
showed all three candidates' updates are ALREADY ~100% HOT at default:

- cursor: 99.99-100% HOT in every cell; batched-claim throughput default
  >= fillfactor 20. Churn is poll-paced (~2k updates/s at 8 groups).
- compaction_head: 99.3-100% HOT; throughput flat at cardinality 1;
  fillfactor 20 CRATERED the 1024-key cell to ~55%. [0574]'s dead
  tuples were HOT-chain tuples pruned in place; the hot-key cost is
  lock-queue serialization, which fillfactor cannot touch.
- delivery: fillfactor 90 raises HOT 64-66% -> 76-77% repeatably, but
  updates are ~8% of the table's writes — 3x2 repetitions showed
  identical throughput and ~10% more bytes/row. The one apparent win
  was checkpoint variance in a single baseline cell.

Tiny hot tables keep mostly-empty pages and opportunistic HOT pruning
recycles dead line pointers during ordinary writes; insert-heavy tables
are dominated by the inserts slack cannot help.

## Consequences

- Baseline DDL untouched; the audit closes with measured confirmation.
- bench/fillfactor stays as the consume-side harness (pre-fill, drain
  through real Consume calls, failure-rate drives exception churn);
  results in its RESULTS.md, raw cells in results/cells.jsonl.
- Re-open only if a workload shows a candidate's HOT ratio actually
  degrading (pg_stat_user_tables n_tup_hot_upd / n_tup_upd is the tell).
- worker_instance's non-HOT heartbeat is an index-design question for
  the idle-fleet worker-load item, not a fillfactor one.
