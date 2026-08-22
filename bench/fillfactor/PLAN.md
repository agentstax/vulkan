# consume-side fillfactor benchmark — plan

Confirms or kills the fillfactor-audit candidates (docs/TODO.md) with live
before/after runs, per the ROADMAP's rule that reasoning alone adopts
nothing. The compaction_head candidate is NOT here — it reruns
bench/compaction's hot-key cells with fillfactor applied.

## What a cell does

Pre-fill a fresh topic (2M unkeyed messages, 3 producers / 128 goroutines,
same shape as bench/compaction), then drain it through real
`ConsumerInstance.Consume` calls — no SQL mirrors. Consumption starts
against a quiet log, so the fillfactor signal isn't muddied by co-tenant
produce load. 10s warmup + 15s steady window; throughput = median of
per-second dispatch samples; a drain guard fails the cell if the backlog
runs out mid-window.

Fillfactor is applied with `ALTER TABLE ... SET (fillfactor)` on the fresh,
empty tables before any rows land — the library DDL stays untouched until a
measured win adopts it (adoption then edits the baseline CREATE TABLEs in
pkg/topic/controller/datastore/tables.go, pre-v1 in-place rule).

## What each axis exercises

- **cursor** — happy path: every claim UPDATEs the group's cursor row
  (claimed / settled_head / pending pair), AdvanceCommitted bumps
  committed. `-batch-limit 1` makes it one cursor update per message
  claimed (maximal churn); 100 is the realistic batched shape. 8 groups =
  8 hot cursor rows sharing pages at default fillfactor.
- **delivery** — `-failure-rate 0.5` marks half the prefilled messages to
  fail every attempt: each writes a delivery row at commit, cycles through
  exception claim / outcome UPDATEs on a short retry curve (250ms base,
  MaxRetries 3), and dead-letters — sustained UPDATE churn on a table whose
  rows were bulk-inserted densely.
- **lease** is reported per cell but has no fillfactor flag: its steady
  state is insert -> delete, and the reclaim UPDATE rotates token (a PK
  column), so fillfactor cannot help it. Its numbers are the control that
  confirms that reading.

## Reading a cell

Beside throughput, each cell reports pg_stat_user_tables for cursor_<id>,
delivery_<id>, lease_<id>: updated vs hot_updated is the direct HOT ratio,
dead_tuples and bytes are the cost side (a lower fillfactor trades table
size for HOT headroom). A fillfactor win must show BOTH a better HOT ratio
and a throughput gain the baseline cell lacks; a bigger table with the same
throughput is a loss, adopt nothing.

Counters are whole-run (fresh tables per cell, warmup included) — ratios,
not absolutes, carry the comparison.

Run: `container.sh start`, then `sweep.sh`. Results append to
results/cells.jsonl; conclusions land in RESULTS.md after the runs.
