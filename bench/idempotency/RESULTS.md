# idempotency_key cost benchmark — stored results

What the default (claim-gated) `Produce` path costs vs `SkipIdempotency`, on a
throwaway postgres:18.4 container (8 cpus / 8GB, shared_buffers=2.5GB,
max_wal_size=4GB, pg_stat_statements, track_io_timing) driven by pgbench 17.9.
Schema mirrored `createTopicLog` exactly: partitioned `message_log` + the real
`idempotency_key` shape (UUID PK + `created_at` DEFAULT NOW() + created_at
index). All throughput numbers are the MEDIAN of per-second samples over a
steady window (15s after 5s warmup; the soak used 230s after 10s). Claim cells
ran with a live janitor-style sweeper (batched `DELETE ... WHERE created_at <
cutoff`) unless noted. Single-row sync=on absolutes are checkpoint-noisy
(±30%) — trends and the batched/sync=off cells are the solid numbers.

The harness itself (scripts, drivers, raw jsonl) is deliberately not tracked —
this document is the durable artifact. Method details worth keeping: 4 arms
(`skip` = bare insert; `claim_v7` / `claim_v4` = the real two-CTE claim+insert
with fresh time-ordered / random keys; `claim_hotkey` = a fixed 100-key pool,
the retry-storm model), swept across clients {8,16,32}, msgs/txn {1,100},
synchronous_commit {on,off}, claim-table sizes {0, 1M, 10M rows}, plus a
4-minute saturation soak and an UNLOGGED-claim-table diagnostic run
(measurement-only — UNLOGGED is categorically not a library option).

## Per-call cost (1 client, sequential, autovacuum off)

Server-side statement time (pg_stat_statements mean) + WAL per message:

| arm      | claim table | srv/call | WAL/msg  | notes                                  |
|----------|-------------|----------|----------|----------------------------------------|
| skip     | —           | 4.2 µs   | 252 B    | floor: heap + idx + commit, 3.0 records|
| claim_v7 | empty       | 8.7 µs   | 511 B    | +4.5 µs, +259 B, 7.0 records           |
| claim_v7 | 1M rows     | 9.3 µs   | 509 B    | flat — right-edge btree insert         |
| claim_v7 | 10M rows    | 9.6 µs   | 509 B    | still flat                             |
| claim_v4 | empty       | 9.4 µs   | 524 B    |                                        |
| claim_v4 | 1M rows     | 11.4 µs  | 1,698 B  | FPI 0.20/msg — scatter starts costing  |
| claim_v4 | 10M rows    | 21.1 µs  | 5,524 B  | FPI 0.84/msg — 2.2x v7 CPU, 10x v7 WAL |

Client-observed latency at 1 client is fsync-dominated (~0.6 ms) and
statistically identical across arms — a single caller cannot feel the gate.

## Throughput ceilings (msgs/s, sweeper live on claim cells)

| cell               | skip   | claim_v7 | claim_v4 | gap (v7) |
|--------------------|--------|----------|----------|----------|
| pc8  b1   sync=on  | 7,176  | 6,341    | 6,286    | −12%     |
| pc16 b1   sync=on  | 12,500 | 10,570   | 10,492   | −15%     |
| pc32 b1   sync=on  | 14,672 | 14,156   | 15,103   | ~0 (fsync-masked, noisy) |
| pc32 b100 sync=on  | 48,300 | 39,900   | 39,800   | −17%     |
| pc32 b1   sync=off | 52,240 | 41,592   | 42,112   | −20%     |
| pc32 b100 sync=off | 48,400 | 40,600   | 39,700   | −16%     |

- Whenever row-work-bound (batched txns, or sync=off), the claim gate costs a
  consistent **16-20% of ceiling**; when fsync-bound it is fully masked by
  group commit.
- WAL volume is **2.3-2.5x** skip (238→~550 B/msg at b1; 205→~518 at b100) —
  the multiplier replication and archiving pay.
- b1 sync=off (52.2k) beat b100 sync=off (48.4k): once flushes leave the
  commit path, shared transactions gain nothing — the entire b1→b100 win at
  sync=on is fsync amortization, not per-txn overhead.

claim_hotkey (100-key shared pool):
- b1: 34-47k attempts/s at near-zero WAL — conflict no-ops are cheaper than
  inserts; single-statement retry storms are self-limiting and harmless.
- b100: **collapse** (~1 txn / 20s, 29-39s avg latency, both sync modes). An
  `ON CONFLICT` insert on a key another open txn speculatively inserted must
  wait for that txn's outcome; 100-statement txns hold 100 key locks for the
  whole batch, so storms serialize globally. Duplicate-suspect produces inside
  long batched transactions are the one true hazard.

## Sweep/vacuum keep-up (claim_v7, pc32 b100 sync=on, TTL=30s, 240s soak)

36,800 msgs/s sustained for 4 minutes (8.77M produced, 7.7M swept live —
~33k deletes/s alongside the inserts). Live rows plateaued at ~1.03-1.06M ≈
rate x TTL = 1.10M (Little's Law holds at the ceiling). Table size went FLAT
at 365MB; dead tuples sawtoothed 0.4-2.0M across 4 autovacuum passes and
always recovered. **Verdict: bounded** — the janitor sweep + default
autovacuum keep up at saturation; steady-state throughput ran ~8% below the
short-window number once the table held its steady-state ~1M rows.

## Decomposition

- UNLOGGED claim table at b1/sync=on recovered the entire gap (18.3k vs 14.2k
  logged, at/above skip's noisy 14.7k) — the b1 cost is WAL-flush-shaped.
- UNLOGGED at b100 recovered nothing (35.4k vs 39.9k logged) — the batched
  cost is CPU/btree row work, consistent with sync=off showing the same gap.
- 10M-row pre-seeded table at b100: claim_v7 37,300 (−6.5% vs small table);
  claim_v4 matched v7's tps (parallelism absorbs the CPU at this core count)
  but paid 894 B/msg WAL vs v7's 476 (1.9x, FPI-driven). The v4 penalty grows
  with table size; v7's cost is flat.

## Conclusions

1. Per-call the claim gate is invisible: ~5 µs server CPU, ~260 B WAL, no
   measurable client latency change.
2. At saturation it costs 16-20% of ceiling and 2.3-2.5x WAL when
   row-work-bound; ~nothing when fsync-bound.
3. The sweep/vacuum backend keeps up at the ceiling with default settings.
4. "Prefer UUIDv7 keys" is validated with numbers: flat cost vs 2.2x CPU /
   10x WAL for random keys at 10M rows. (Doc comment updated with these.)
5. Hot-key storms: harmless as single statements, catastrophic inside batched
   transactions — batching must exclude caller-supplied keys or coalesce them.
6. Decisions taken from these results: `IdempotencyKeyTTL` default lowered
   24h → 1h (steady-state table = rate x TTL; the TTL only needs to cover the
   retry horizon); producer batching pursued via a payload-only entry point
   (one txn per batch, whole-batch retry — safe BECAUSE every message carries
   a claim); `SkipIdempotency` removed rather than documented as the
   high-throughput escape hatch.

Deferred (not run): multi-topic contention, partition rollover under load,
streaming-replica amplification, commit_delay points, ProduceInTx savepoint
overhead.

## Follow-up: in-library batched Produce (acceptance lab)

The library-side answer to conclusion 6 — measured through the real public
API once batching landed, via `examples/phase_1/producerbatchlab`
(`just producer-batch-lab`), while `SkipIdempotency` still existed as the
comparison floor. Environment differs from the container above: the dev
postgres:17 under Docker Desktop on macOS (fsync=on, synchronous_commit=on,
untuned). `common.Work` payload, default knobs (BatchMaxSize 100 /
BatchConcurrencyLimit 4), pool warmed untimed. The three comparison arms
share one caller count (50 goroutines x 400 msgs) so the ratio is fair; the
saturated arm is the batcher's actual ceiling.

| arm                                        | msgs/s (multi-run) |
|--------------------------------------------|--------------------|
| batched `Produce` (claim-protected)        | 20,000 – 29,900    |
| per-call `ProduceFunc` (claim-protected)   | 11,100 – 12,400    |
| per-call `SkipIdempotency` (floor)         | 11,100 – 13,500    |
| batched `Produce`, saturated (800 callers) | 108,800 – 134,600  |

- The equal-concurrency arms are ARRIVAL-bound, not commit-bound: N callers
  each blocked ~one commit cap the offered load at N/latency (Little's
  law) — at 50 callers that's ~25k/s no matter how fast the batcher is.
  Sweeping callers on the batched arm showed exactly that: 50 → ~20-23k
  (largest batch 49), 100 → ~33k (99), 200 → ~67-77k, 400 → ~109-111k,
  800 → ~127-135k, batches pinned at the 100 cap from 200 callers up.
- So the honest multiples: **1.8-2.5x** the per-call paths at equal
  concurrency, **~10-12x** the unprotected per-call floor once callers
  saturate the batch cap. The protected batched path doesn't just
  compensate for `SkipIdempotency`'s removal — it laps the path skip
  existed to preserve.
- Per-call protected ties the skip floor at 50 concurrent callers — the
  claim gate is fully fsync-masked per-call in-library too, matching
  conclusion 2's fsync-bound case.
- Commit-driven grouping self-scales batch size to arrival rate with no
  linger timer: ~13-48 at 50 callers, the full 100 cap at 200+; still
  size 1 (zero added latency) at idle.
- `BatchConcurrencyLimit` sweep {1, 2, 4, 8} at 50 callers: flat, within
  run noise — arrival-bound traffic never backs up the queue enough to
  spawn extra workers. Default stays 4 (headroom for slow-commit
  environments and saturated bursts, not this box).
