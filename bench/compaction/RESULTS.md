# compaction hot-key serialization benchmark — stored results

What a hot compaction key costs on the DEFAULT (batched) produce path, on a
throwaway postgres:18.4 container (8 cpus / 8GB, shared_buffers=2.5GB,
max_wal_size=4GB, pg_stat_statements, track_io_timing; driver co-tenant on
the same machine, like the other benches). Unlike bench/idempotency's pgbench
mirrors, the driver hammers the REAL `ProducerInstance.Produce` — the
batcher's ascending-key sort and batch sharing are the thing under test, so
a SQL mirror would measure the wrong path and go stale.

Method: 128 goroutines calling Produce (no caller idempotency key → the
batcher; defaults MaxSize=100, ConcurrencyLimit=4 per producer instance),
keys rotated from per-goroutine offsets over a fixed pool, fresh topic per
cell, 5s warmup + 15s steady window; throughput = MEDIAN of per-second
samples, latency percentiles over every measured Produce call. One rep per
cell (single-cell sync=on absolutes carry checkpoint noise — trends are the
solid signal; two cells show a stalled p10 sample from a checkpoint, their
medians unaffected). Raw cells: results/cells.jsonl; harness: driver/ +
sweep.sh; container: container.sh.

Every cell raised ZERO deadlocks (pg_stat_database.deadlocks, asserted by
the driver) — the batcher-sort absence claim held at bench scale, matching
examples/phase_1/compactiondeadlocklab.

## Cardinality curve — 3 producers, sync=on

| keys    | msgs/s med | vs unkeyed | p50 ms | p99 ms | head dead tuples/15s |
|---------|-----------:|-----------:|-------:|-------:|---------------------:|
| unkeyed |     50,856 |       100% |    2.3 |    5.6 |                    0 |
| 1024    |     35,263 |        69% |    3.3 |    7.8 |                3,158 |
| 64      |     24,406 |        48% |    4.9 |   56.0* |               1,056 |
| 8       |     24,458 |        48% |    5.1 |    8.8 |                1,498 |
| 2       |     24,239 |        48% |    5.1 |    9.0 |                3,320 |
| 1       |     25,333 |        50% |    4.9 |    9.0 |               36,431 |

*checkpoint stall sample; the cell's median is in line with its neighbors.

The shape: keyed throughput steps down ONCE (~30% by 1024 keys — mostly the
head upsert's second write, the cost compaction-head-write-lab measured),
then FLATTENS at ~48-50% of unkeyed all the way down to one single hot key.
No cliff: full serialization behind one head-row lock still commits ~250
batches/s × ~100 messages. Batch amortization is what makes the worst case
a floor instead of a collapse.

## sync=off — same cells (isolates fsync from lock queuing)

| keys    | msgs/s med | vs unkeyed |
|---------|-----------:|-----------:|
| unkeyed |     94,439 |       100% |
| 1024    |     55,585 |        59% |
| 64      |     46,042 |        49% |
| 8       |     48,463 |        51% |
| 2       |     49,593 |        53% |
| 1       |     51,054 |        54% |

Removing fsync roughly doubles absolutes but leaves the RELATIVE keyed floor
at ~50% — the penalty is lock-queue serialization plus the double write, not
commit latency. A faster disk does not buy the hot key back.

## Producer count into ONE hot key — sync=on, 128 goroutines total

| producers | keyed msgs/s | unkeyed msgs/s | keyed p99 ms | head dead tuples/15s |
|----------:|-------------:|---------------:|-------------:|---------------------:|
|         1 |       25,432 |         34,093 |         26.2* |               2,087 |
|         3 |       25,333 |         50,856 |          9.0 |               36,431 |
|         6 |       10,492 |         30,223 |        130.6 |               14,477 |

*single-producer cells show checkpoint-wide p10/p99 spread.

1 → 3 producers: the hot-key ceiling is flat (~25k) — adding batchers
neither helps nor hurts, as the sort predicts. At 6 producers (24 concurrent
batch transactions fighting one row lock) it INVERTS: throughput drops 2.4×
below the 1-producer case and p99 blows out to ~130ms, while the unkeyed
control only dips mildly (same-host driver overhead). Horizontal producer
scale is where a hot key genuinely hurts, and the tail hurts before the
median does.

## Conclusions

- Deadlocks: never, at any cardinality or producer count — self-heal is not
  even needed on the batched path; the sort prevents the cycle outright.
- One hot key costs ~half your unkeyed throughput and ~2× p50 latency — a
  FLOOR (batch amortization), reached already at cardinality ~64 under this
  concurrency, not a cliff at 1.
- The floor is structural (lock queue + head double-write): sync=off moves
  absolutes, not the ratio.
- The real hurt case is hot key × many producer processes: past a handful of
  batchers the lock queue turns into tail-latency collapse (p99 >100ms) and
  falling throughput. Guidance for ProduceOptions.Compaction docs: a hot key
  caps batched throughput at roughly half, and scaling producers past ~4
  makes a hot key WORSE, not better.
- A single busy head row accrues dead tuples fast (36k/15s at 3 producers,
  sync=on) — feeds the fillfactor-audit roadmap item (compaction_head is
  already on its candidate list).
