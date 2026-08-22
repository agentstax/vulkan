---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# Compaction-key deadlock evaluation: no cycles batched, no library retry

## Context

Every keyed publish upserts its key's compaction_head row, holding that
row's lock until the transaction commits. The open question (ROADMAP Now):
can reverse-ordered transactions deadlock, and what does hot-key lock
queuing cost on the default batched path -- does it truly hurt users, or
does the system self-heal?

## Decision

Evaluated empirically, no mechanism changes. Three findings:

1. Batched Produce cannot deadlock -- by construction, and now by test.
   The batcher sorts every batch ascending by compaction key, one global
   lock order across all batchers. compactiondeadlocklab (3000 produces,
   120 goroutines, 8 hot keys) and every bench cell (up to 24 concurrent
   batch transactions on ONE key) raised zero pg_stat_database.deadlocks.
2. ProduceInTx is the one deadlock site, and it stays the caller's to
   retry. Reverse-ordered InTransaction callers deadlock; Postgres kills
   one after deadlock_timeout (~1s -- the user-visible spike), the 40P01
   classifies transient, and rerunning the closure lands both (the lab
   demonstrates the loop). A library retry on the provably-rolled-back
   codes (40P01/40001, crdb.ExecuteTx precedent) was proposed and
   REJECTED: keep InTransaction simple and honest -- it never reruns the
   caller's closure. Documented guidance instead: order ProduceInTx calls
   by compaction key (the batcher's own order, so mixed traffic composes).
3. Hot-key cost is a floor, not a cliff (bench/compaction/RESULTS.md).
   One hot key = ~50% of unkeyed throughput and ~2x p50 latency, flat
   from cardinality ~64 down to 1 -- batch amortization (~100 messages
   per serialized commit) is what prevents collapse. The ratio survives
   sync=off (lock queue + head double-write, not fsync). The genuine
   hurt case is hot key x producer-process count: 6 batchers on one key
   drop 2.4x below one batcher with p99 >100ms.

## Consequences

- examples/phase_1/compactiondeadlocklab joins the lab suite; RETRY-40P01
  in TEST.md now describes the caller-side retry and points at it.
- bench/compaction (container.sh / driver / sweep.sh, RESULTS.md the
  durable artifact) joins bench/; folded into the benchmark-recording
  standard when that lands.
- ProduceOptions.Compaction and ProduceInTx doc comments carry the
  qualitative guidance; the numbers live in RESULTS.md and here.
- The hot head row's dead-tuple rate (36k/15s at 3 producers) is recorded
  input to the fillfactor audit, which already lists compaction_head.
- Never re-propose: library-side InTransaction retry.
