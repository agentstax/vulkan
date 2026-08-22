# fillfactor audit benchmarks — stored results

Live confirmation runs for the fillfactor audit's candidates, on the
bench/ convention container (postgres:18.4, 8 cpus / 8GB,
shared_buffers=2.5GB, sync=on; driver co-tenant on the same machine).
Method in PLAN.md; raw cells in results/cells.jsonl (the head-ff* cells
ran through bench/compaction's driver with its -head-fillfactor flag).

VERDICT: adopt nothing — every candidate's updates were already ~100%
HOT at default fillfactor, so page slack had nothing left to buy. The
baseline DDL keeps default fillfactor on every table. [0578]

## cursor — no change

The claim / advance UPDATEs were 99.99-100% HOT at DEFAULT fillfactor in
every cell — the table is rows-per-page tiny, and opportunistic HOT
pruning keeps its pages workable without reserved slack.

| cell (8 groups) | fillfactor | msgs/s med | cursor HOT |
|-----------------|-----------:|-----------:|-----------:|
| batch-limit 100 |    default |    153,430 |     100.0% |
| batch-limit 100 |         20 |    143,400 |      99.9% |
| batch-limit 1   |    default |        775 |     100.0% |
| batch-limit 1   |         50 |        976 |     100.0% |
| batch-limit 1   |         20 |      1,238 |     100.0% |

The batch-1 medians drift upward but their p10-p90 spreads (~400-2,000)
overlap almost entirely — that axis is claim-poll-pacing-bound, and the
batched cells show default >= lowered. Cursor churn is also poll-paced,
not message-paced: ~30-33k cursor updates per cell at BOTH batch limits.

## delivery — no change

failure-rate 0.5: half the messages fail every attempt and cycle the
exception window (insert at commit, claim / outcome UPDATEs, dead after
3 retries). Single cells first suggested a fillfactor-90 win; three
repetitions showed it was checkpoint variance.

| rep | ff default med | ff 90 med | ff default HOT | ff 90 HOT |
|-----|---------------:|----------:|---------------:|----------:|
| 1   |        100,865 |   104,738 |          65.7% |     76.2% |
| 2   |         95,788 |    94,971 |          65.6% |     76.2% |
| 3   |         97,883 |    97,158 |          63.9% |     77.2% |

The HOT gain (~64-66% -> ~76-77%, 90% at fillfactor 70) is real and
repeatable — and buys nothing: updates are only ~8% of the table's
writes (~1M inserts vs ~80-100k updates per cell), so inserts dominate
and throughput is identical within noise. The cost side is real too:
~130 bytes/row at default vs ~142 at 90 vs ~181 at 70. A bigger table
for the same throughput is a loss; adopt nothing.

## compaction_head — no change

Rerun of bench/compaction's hot-key cells (3 producers, 128 goroutines,
sync=on) with -head-fillfactor. [0574]'s 36k dead tuples/15s were
HOT-chain tuples pruned in place all along:

| cardinality | fillfactor | msgs/s med | head HOT | head dead tuples |
|------------:|-----------:|-----------:|---------:|-----------------:|
|           1 |    default |     19,593 |    99.3% |           28,302 |
|           1 |         50 |     20,888 |    99.3% |           10,139 |
|           1 |         20 |     21,019 |    99.4% |            1,159 |
|        1024 |    default |     20,973 |    99.9% |            1,297 |
|        1024 |         50 |     21,715 |   100.0% |            2,641 |
|        1024 |         20 |     11,585 |   100.0% |            1,208 |

Throughput is flat within noise at cardinality 1; the lone big mover is
the cardinality-1024 fillfactor-20 cell CRATERING to ~55% of baseline (a
1024-row table spread over 5x the pages, plus checkpoint exposure).
Lower fillfactor reduces the resident dead-tuple count but the hot-key
cost was never dead tuples — it is lock-queue serialization ([0574]).

## lease — control confirmed

Zero updates in all 14 cells: steady state is insert (claim) -> delete
(commit), which fillfactor cannot help — matching the static audit that
ruled it out (the reclaim UPDATE also rotates token, a PK column).

## Why default fillfactor was already enough

HOT needs the update to avoid indexed columns (true on all candidates —
the static audit's filter) AND same-page room for the new version. These
tables' hot rows either sit on mostly-empty pages (cursor,
compaction_head: a handful of small rows per active page) or churn
through insert-heavy lifecycles where updates are a small minority
(delivery, lease). Postgres's opportunistic HOT pruning then recycles
dead line pointers during ordinary writes, so pages never fill to the
point of forcing cold updates. Reserved slack solves a problem these
workloads do not have.
