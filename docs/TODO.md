# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Compaction-key deadlock evaluation (ROADMAP Now)

Three claims, cheapest first. Durable artifact: results doc (bench-style
RESULTS.md or the decision record), not the harness.

1. **Confirm the absence claim** — DONE 2026-08-22: `compactiondeadlocklab`
   (`just compaction-deadlock-lab`). 120 goroutines across 3 producer
   instances, 3000 batched produces over 8 hot keys, each goroutine walking
   the pool from a different offset — pg_stat_database.deadlocks flat
   (catches 40P01s absorbed by DatastoreRetry.Wrap, not just surfaced ones),
   every produce landed, heads converged to max id. Validates the batcher
   sort comment (pkg/producer/batcher/batch.go) empirically.
2. **Measure the serialization cost** — hot compaction keys queue batch
   commits behind the compaction_head row lock. Bench (bench/compaction/):
   sweep key cardinality down toward 1 hot key; chart throughput/latency
   against the uncompacted baseline. Output: the "at what example does it
   truly hurt users" number the roadmap item asks for. Overlap check:
   compaction-head-write-lab already measures one-hot-key contention on the
   unbatched path — the bench covers the batched path and the cardinality
   curve.
3. **Confirm the self-heal claim for ProduceInTx** — DONE 2026-08-22, same
   lab: two InTransaction callers took two keys' head locks in reverse order
   behind a barrier; Postgres killed exactly one (40P01 after ~1.01s
   deadlock_timeout — the user-visible latency spike), the error classified
   transient, and a caller-side rerun (InTransaction does not retry — its
   docs leave the loop to the caller) landed both. Note TEST.md RETRY-40P01
   says "Wrap retries it" — inaccurate, the retry is the caller's; fix the
   TEST.md wording when 14c picks that file up.
   USER-SETTLED 2026-08-22: InTransaction stays no-retry — a library retry
   on the provably-rolled-back classes (40P01/40001) was proposed and
   rejected for simplicity; the caller owns the loop. Goes in the decision
   record.

Deliverable beyond numbers: the decision record must state the ProduceInTx
mitigation — callers producing multiple compaction keys in one transaction
should order their ProduceInTx calls by compaction key (matches the batcher's
global ascending order).
