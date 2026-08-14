---
status: accepted
date: 2026-08-14
phase: "14"
---

# 0512 — Producer create-ahead triggers at the partition's 80% mark id; heal path serialized by advisory lock

**Context.** The janitor's proactive partition creation was deleted with
pkg/maintain ([0428]): creation is the write path's job, and the reactive
`ensureCoveringPartition` at the boundary is the only correctness layer. That
leaves two costs: every boundary insert eats a failed-insert/DDL/retry latency
spike, and every produce in flight at a boundary runs the heal concurrently —
a thundering herd of identical DDL.

**Decision.** The append paths themselves create the next partition early,
best-effort:

- Trigger: each append checks whether its returned id range contains the
  containing partition's 80% mark — partition `n` covers
  `[n*size, (n+1)*size)`, mark = `n*size + size*8/10`. Ids come from one
  sequence, unique fleet-wide, so exactly one process observes the mark id;
  no coordination. The check is a few integer ops per call, no SQL.
- Home: the producer datastore — the one layer where all three paths
  (single, batch, in-tx) surface ids, and where `ensureCoveringPartition`
  already lives. The in-tx path triggers pre-commit: a rollback burns the id
  regardless, and an early empty partition is harmless.
- Intra-process gate: a per-topic `atomic.Int64` holding the highest
  partition already attempted, advanced by CAS — monotonic, never reset, so
  a long-lived process keeps creating ahead partition after partition while
  pipelined batches straddling one mark can't double-run the DDL.
- The creation itself runs in a goroutine (background ctx + its own timeout;
  the produce ctx is dead once the caller returns) as
  `DatastoreRetry.Wrap(ensureCoveringPartition)` — unchanged math: at 80% of
  partition `n`, `MAX(id)/size + 1 = n+1`. On final failure: warn and drop.
  A failed attempt never re-runs for that partition; the boundary heal is
  the only layer allowed to matter for correctness.
- Herd cap on the heal path: `ensureCoveringPartition` takes a blocking
  `pg_advisory_xact_lock` (single-bigint mixed key of topic id + partition)
  before the CREATE, inside the existing implicit-transaction batch and
  under the existing 2s `lock_timeout` (which bounds advisory waits too).
  One winner does DDL; losers sleep until the winner commits, their
  `IF NOT EXISTS` no-ops, and the one-shot heal callers stay one-shot.

**Consequences.** Under steady traffic partitions exist before the boundary,
so the boundary-heal warn becomes the signal that create-ahead misfired.
Misses are by design lossy: a rolled-back transaction can burn the mark id
unobserved — the heal covers it. If the drop warn shows up in practice, the
answer is a second mark at ~95%, not retry state. **Rejected:**
`pg_try_advisory_xact_lock` with instant-returning losers — a loser could
retry its insert before the winner's CREATE commits, and the heal callers
are deliberately one-shot, so instant return forces retry-loop machinery
into both; blocking bounded by `lock_timeout` keeps them unchanged. Also
rejected: a boolean attempted flag needing a reset after success — the
monotonic watermark makes reset a non-question.
