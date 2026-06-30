# Reference benchmark vs. the SQL/pgbench targets

Run on the throwaway Postgres 18 container (`vulkan-bench-pg`, `--cpus=8
--memory=8g`, `shared_buffers=2GB max_wal_size=4GB`) on Docker-for-Mac, via:

```
go run ./reference/waterline/cmd/bench \
  -events 1000000 -workers 16 -batch 500 -lanes 16 -groups 8 \
  -exceptions 500000 -exc-batch 1000
```

The ranges below are observed over several runs. **Variance is large** (a shared
8-core box + Docker-for-Mac fsync/scheduler jitter — e.g. single-lane was seen at
both 108k and 232k depending on background load), so read the *ratios and orders
of magnitude*, not the exact figures. The **SQL target** column is the
pgbench/SQL-harness number from `bench/scale/results/FINDINGS.md`.

| path | Go reference (units/s) | SQL target | verdict |
|---|---|---|---|
| append (COPY, batched) | 851k – 918k | 21k single-row → **922k** batched | ✅ matches the batched ceiling |
| happy: single-lane (frontier-bound) | 108k – 232k | ~136k | ✅ same order; straddles the target |
| happy: sharded K=16 (escape hatch) | 597k – **860k** | 136k → 521k | ✅ exceeds; sharded ≫ single |
| happy: multi-group G=8 (aggregate) | 348k – 758k | 460k – 770k | ✅ in/near band |
| exception: per-message `Ack` | **~3.5k – 4.5k** | (fsync-bound) | ⚠️ the commit wall, by design |
| exception: batched `AckBatch` (b1000) | 97k – 133k | ~258k | ◑ same order; 2-commit path (see caveats) |
| exception: fused `DrainPopDelete` (b1000) | 164k – **282k** | ~258k | ✅ matches the fused target |

## What this confirms (the architecture-level claims, not the absolute numbers)

1. **Claim-from-log ≫ per-row.** The happy path (sharded/aggregate 350k–860k)
   runs several× the exception per-row path (100k–280k) and ~100× the per-message
   path (~4k). This is the entire thesis of the hybrid: pay the per-row cost only
   for the exceptional fraction.
2. **Sharding lifts a single hot group.** single-lane → K=16 sharded is ~3–5×
   (the SQL audit measured +280%). The frozen-block lanes (R1/R4) do what the
   escape hatch promised.
3. **pop-delete ≫ the 2-commit path.** Fused/batched pop-delete (100k–282k) beats
   per-message ack (~4k) by ~25–70×. The SQL audit's "pop-delete > 2-UPDATE,
   +134%" direction holds (and then some, because per-message ack here pays a full
   commit/fsync per message).

## Caveats — where this is NOT apples-to-apples with the SQL numbers

These matter; the verdicts above are "same shape / order of magnitude," not
"identical measurement." Disclosed so the comparison isn't overstated:

- **Drain-to-empty wall-clock, not steady-state windowed throughput.** The Go
  harness seeds a fixed backlog and times how long N workers take to drain it to
  empty; the SQL targets are steady-state rows/s measured over a window while the
  log keeps growing. The Go number includes ramp-up and tail (the last workers
  finding empty lanes), so it is a *conservative* readout of peak throughput.
- **The happy path here is d5 (lease-per-batch), the targets are d4 (no lease).**
  This reference's `Claim` writes a `leases` row and `Commit` deletes it — that is
  the crash-safe range-lease (d5), two extra writes per batch the pure
  "claim-from-log" (d4) SQL numbers (481k/768k) did not pay. That the Go d5 path
  still reaches/exceeds the d4 targets is down to `batch=500` amortising the
  round-trips and PG18 `old/new RETURNING` making the claim a single cheap UPDATE;
  it is *not* evidence that d5 is free — it is the same order of magnitude despite
  the extra lease writes.
- **Batch size differs (b500 here vs b200 in parts of the SQL ladder).** Larger
  batches amortise more round-trips, so the sharded/single happy numbers are
  partly a batch-size effect, not purely the design. Re-run with `-batch 200` to
  compare like-for-like.
- **COPY ≠ batched INSERT for append.** The 851k–918k seed rate uses `CopyFrom`
  (binary COPY), which is faster than the multi-row INSERT the "922k batched"
  producer ceiling refers to, and the measurement includes a trailing `Head()`
  round-trip. Treat "matches the batched ceiling" as "same order," not identical.
- **The crash-safe exception path is two commits (claim→inflight, then
  `AckBatch` delete); the SQL `pop-delete` fused both into one.** That ~2× is
  exactly the 97k–133k (batched) vs 164k–282k (fused) gap. The fused
  `DrainPopDelete` is the apples-to-apples comparison to the 258k target and lands
  on it; the batched path is the crash-safe default and pays one extra commit.
- **Per-message `Ack` at ~4k is the point, not a regression.** One DELETE = one
  commit = one fsync per message — precisely LEARNING_PLAN Phase 3.5's
  "fsync-per-commit is the throughput wall." The fix is the same: batch the acks
  (`AckBatch`, +25×) or relax `synchronous_commit` (safe under at-least-once).

**Bottom line:** the reference reproduces the benchmark's *shape* — happy path in
the hundreds-of-thousands to ~860k, exceptional path the ~100k–280k fallback,
per-message commit the wall you escape by batching — with the absolute gaps named
and explained rather than papered over.
