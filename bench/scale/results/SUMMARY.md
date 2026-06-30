# At-scale end-to-end benchmark — running findings

Throwaway container `vulkan-bench-pg` (postgres:18.4, --cpus=8 --memory=8g, shared_buffers=2.5GB,
max_wal_size=4GB), :5433. pgbench/psql 17.9 on host. Co-tenant: dev DB on :5432 (idle). All numbers
are MEDIAN of per-second samples over a 20s steady window after 6s warmup, unless noted. Variance at
single-row sync=on points is high (checkpoint/WAL cycles) — p10/p90 spreads noted; treat b1/synON
absolutes as ±30%, trends as solid.

## Phase 1 — Producer side (append ceiling + d1 synchronous trigger fanout tax)

### Bare append (no fanout) — shared by d4/d5 and the producer side of d2/d3
Client scaling (pbatch=1, sync=on), events/s:
  pc1 1.7k · pc4 2.2k · pc8 3.9k · pc16 8.5k · pc24 10.2k · pc32 ~11.6k · pc48 20.1k · pc64 20.8k
  -> rises to ~21k ev/s single-row; knee ~pc48 (8 container cores, fsync-bound). Diminishing past 48.

Batch scaling (pc32, sync=on), events/s:
  b1 ~12.8k · b10 75k · b100 533k · b1000 922k        -> batching is the #1 lever (amortizes fsync)

synchronous_commit=off (pc32):  b1 47.9k (~3.7x vs on) · b100 997k (~1.9x vs on)
group commit commit_delay (pc32,b1,synON): cd50 17.3k (+35%) · cd100 15.7k · cd200 13.7k
  -> commit_delay=50us is a real win for single-row appends; larger delays regress.
UNLOGGED ceiling (pc32,b100): 1.45M/s (synON==synOFF, no WAL) -> absolute insert ceiling ~1.5M ev/s

### d1 — synchronous statement-level trigger fanout (pc32, b1, sync=on)
events/s (and delivery-rows/s = events x groups):
  g1   19.1k  (19.1k dlv/s)   [within noise of bare b1; 1-group trigger overhead is small]
  g10  9.7k   (97k dlv/s)
  g50  4.4k   (218k dlv/s)
  g100 3.7k   (371k dlv/s)
  g500 0.94k  (471k dlv/s)
sync=off: g10 19.2k (~2x) · g100 4.4k (~1.18x) · g500 0.97k (~1x)   -> at high fanout it's ROW-VOLUME
  bound, not fsync bound, so sync=off & batching help less and less.
batched b100: g10 49.7k ev/s (497k dlv/s) · g100 5.1k ev/s (509k dlv/s)

KEY: the trigger's delivery-row write throughput PLATEAUS around ~470-510k rows/s on this hardware
regardless of group count / sync / batch. That ~0.5M delivery-rows/s is the materialization wall for
the synchronous-trigger approach. Event-append rate = 0.5M / groups. Fanout tax grows ~linearly in
delivery-row volume once you're past a handful of groups.

## Phase 2 — Materialization via projector (d2/d3)
Materialize ceiling (delivery-rows/s, static backlog, groups=10):
  offset-window batch (1 shard): b1000 290k · b5000 311k · b20000 342k · b50000 338k  (plateau ~b20000)
  parallel shards (b20000):      n1 342k · n2 526k · n4 666k(peak) · n8 629k  (scales to 4 cores, 8 regresses)
  groups=100 (b20000):           n1 308k · n4 558k(peak) · n8 545k   (~same as g10)
keep-up vs live producer (4 shards): poll lag=180 events · listen lag=430 events  -> both keep up;
  proj rate == append x groups exactly. BUT 4 flat-out projectors + per-statement NOTIFY throttle the
  producer to ~5k ev/s (real producer/projector contention when both run hot).
=> async materialize ceiling ~666k rows/s ~= the sync-trigger ~500k plateau, but it DECOUPLES fanout
   from the producer commit path (producer can append at bare speed; fanout happens off the hot path).
   poll vs listen: ~equivalent throughput; listen has lower idle latency, poll is simpler.

## Phase 3 — Consumer DRAIN ceilings (units/s = (group,event) pairs acked/s)

### Per-row deliveries claim+ack (shared by d1/d2/d3), groups=10
clients (b200, synON): cc8 116k · cc16 116k (peak) · cc32 106k · cc48 86k  -> knee ~cc16, then
  contention regresses.
batch:   b50 76k · b200 116k · b1000 141k   (batching the claim/ack helps ~+20%)
levers that DON'T help (the surprise): sync=off 113k (~same as on!) · combined 1-txn claim+ack 51k
  (WORSE — longer lock hold => more SKIP LOCKED contention).
=> per-row drain is ROW-THROUGHPUT bound (~116-141k/s), NOT commit/fsync bound. Writing 2 row-versions
   + index upkeep per delivery is the wall. This is the consumer-side cost of materialization, and the
   usual fsync levers do not rescue it. (NOTE: original ladder rows 6-9 collapsed to ~0 from a
   checkpoint storm caused by rapid 7M-row reseeds; re-run cleanly in drain_retry.jsonl. Seeder now
   CHECKPOINT+ANALYZEs.)

### claim-from-log (d4) — no per-event rows
groups=1 (single frontier): cc8 128k · cc16 119k · cc32 105k  -> CONTENTION-BOUND on the one cursor
  row; adding clients doesn't help (high variance, p10 as low as 8k). This is the single-frontier wall.
groups=10:  ~350-490k (b200), exhausted backlog at b1000/synOFF (>=2.5M/s avg).
groups=100: cc32 b200 1.06M · cc32 b1000 4.76M · cc32 b200 synOFF 2.03M  (clean, no exhaustion)
=> claim-from-log drain scales with group count (spreads frontier contention) and batch; ceiling is
   1-5M units/s — 10-40x the per-row deliveries drain. The cost is just a cursor advance per batch.

### range-lease (d5), groups=10
cc8 439k · cc16 511k (peak) · cc32 353k (b200); b1000 2.4M (exhausted); synOFF 1.5M
=> ~same order as d4 g10, slightly lower (extra lease INSERT+DELETE per batch). Far above per-row drain.

HEADLINE (drain): no-fanout designs (d4/d5) drain 4-40x faster than per-row deliveries (d1/d2/d3),
mirroring the producer side. Materialization costs a row write on BOTH produce and consume.
*** AUDIT CORRECTION (see Phase 5): the d4/d5 reads above were an index-only count(*) (no heap/payload
read) AND several were exhausted-backlog burn-downs. Corrected (count(payload) heap read + replicated,
non-exhausting) numbers are in Phase 4/5 and drain2.jsonl; the matched apples-to-apples advantage is
~3-8x, not 40x. The 40x mixed in higher group count + bigger batch. Direction holds; magnitude corrected.

## Phase 4 — END-TO-END (producer + consumer + projector concurrent), groups=10, pc16/cc16/pbatch10
d4/d5 use the CORRECTED heap-read; backlog/diverging valid (reset.sh resets progress per design).
Units/s = (group,event) pairs fully acked/s; events/s fully processed = units/s / 10.

Saturation (producer unthrottled):
  d1 (sync trigger):   append 19.5k ev/s, complete 75k units/s  (7.5k ev/s fully) -- consumer-bound
  d2 (notify+proj):    append 3.4k,       complete 34k          (3.4k)  -- producer contention-throttled
                       (per-statement NOTIFY + 4 projectors + consumers all contend; producer starved)
  d3 (poll proj):      append 26.9k,      complete 43k          (4.3k)  -- projector steals CPU from consumer
  d4 (claim-from-log): append 68.7k,      complete 287k         (28.7k) -- ~4-8x the materialize designs
  d5 (range-lease):    append 72.8k,      complete 300k         (30k)

Breaking point (offered events/s before backlog diverges, g10):
  d1 ~5-10k · d2 capped ~3.4k (can't push harder) · d3 ~5-10k · d4 ~20-40k · d5 ~20-40k
  => no-fanout sustains ~3-6x higher offered load end-to-end.

Spill tier (pre-grow hot table to 10M rows, saturate):
  d1 75k->56k (-26%) · d4 287k->270k (-6%) · d5 300k->257k (-14%)
  => log-claim (range index scan) degrades far less with table size than per-row deliveries claim.
  CAVEAT: 10M rows (~1.5GB) still fits the 8GB container cache, so this is table-size/index effect, NOT
  true disk spill. (d2/d3 spill runs pre-materialized deliveries => projector idle => they measured pure
  drain on a 10M table, not e2e; excluded from comparison.)
