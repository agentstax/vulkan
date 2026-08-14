# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Producer proactive partition create-ahead [0512]

Design settled 2026-08-14 — mechanism and rejected shapes in the record.
Chunks in order; each ships on its own.

- [x] **Chunk 1 — advisory lock in `ensureCoveringPartition`** (heal herd
  cap; hardens the existing path regardless of the rest). Built 2026-08-14.
  - Blocking `pg_advisory_xact_lock`, single-bigint mixed key of
    (topic id, next partition), queued into the existing implicit-txn
    `pgx.Batch` after `SET LOCAL lock_timeout` — the 2s timeout bounds the
    lock wait; losers wake after the winner commits and `IF NOT EXISTS`
    no-ops. Key uses the `next` the function already computes.
  - Verify: build, `go test -race` on pkg/producer/..., partitionlab.
- [ ] **Chunk 2 — trigger + per-topic gate + async creation** in the
  producer datastore.
  - One method: mark math (`partition = firstId/size`,
    `mark = partition*size + size*8/10`, contained in [firstId, lastId]),
    per-topic `atomic.Int64` highest-partition-attempted advanced by CAS
    (monotonic, no reset), then a goroutine — background ctx + timeout,
    `DatastoreRetry.Wrap(ensureCoveringPartition)`, warn and drop on final
    failure. Goroutine may be cut by process exit — fine, best-effort.
  - Call sites: `appendMessage`, `AppendMessageInTx` (pre-commit),
    `appendMessageBatchTransaction` (first/last returned id). Duplicates
    return Id 0 — never match the mark.
  - Verify: `go test -race` on pkg/producer/..., partitionlab +
    producerbatchlab.
- [ ] **Chunk 3 — lab coverage + close-out.**
  - Drive produces across the 80% mark; assert the next partition exists
    BEFORE the boundary insert and the "no partition covers" warn never
    logs; cover batch and in-tx paths. Extend partitionlab first; a
    separate lab only if it doesn't fit — decide at build.
  - Close-out: HISTORY.md entry citing [0512]; remove this section and the
    ROADMAP line.
