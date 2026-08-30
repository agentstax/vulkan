---
status: accepted
date: 2026-08-30
phase: pre-v1
---

# 0620 — a missing-partition heal creates the partition of the sequence's next id and reruns under the retry policy

## Context

producerbatchlab's partition-heal scenario failed one run in four with
`no partition of relation "message_log_91" found for row` *after* the
heal. The heal created one partition, `MAX(id)/partitionSize + 1`, and
the batch retried exactly once, a miss past that being "terminal". But a
batch's ids are not aligned to partition boundaries: with head at 19,
eight concurrent batches heal partition 2, and the retry that draws ids
28-32 lands 28, 29 and fails on 30 -- partition 3. Every failed attempt
also burns one sequence value, pushing reruns further past what
`MAX(id)` sees. The lab passed only when the 80% create-ahead goroutine
won the race. The failed row's own id is burned with its rollback, so
the question is where the rerun's id will land -- and only the
sequence knows that.

## Decision

- `ensureCoveringPartition(ctx, topicId, partitionSize, id)` creates the
  partition covering `id`. The heal (`createNextIdPartition`) reads
  `last_value` from `message_log_<id>_id_seq` and covers `last_value +
  1` -- every id already drawn is at or below it, so the rerun's fresh
  id is at or past it; create-ahead passes the trigger id and creates
  the partition after its own. The `MAX(id)` read is gone.
- `insertUntilCovered` is the one heal loop all three append paths run:
  insert; while routing fails, create the partition covering the
  sequence's next id and rerun at once (the partition exists now --
  there is nothing to back off from). Bounded at one heal for the
  boundary itself plus one per `Retry.MaxRetries`; exhaustion is
  `errPartitionCreationBehind` (VK0056, Permanent -- an unchanged retry
  is exactly what just failed).
- The heal warn line is declared (VK0057, `eventPartitionCreatedOnInsert`)
  beside VK0033 and carries `message_id`.

## Consequences

- A straddling batch heals twice and lands; the lab's premise "cap <=
  PartitionSize so one heal covers a whole batch" is deleted.
- The batch path no longer wraps its heal in a retry of its own: a heal
  lock timeout fails the produce fast on every path, as VK0018's note
  already said for the single-row paths.
- Rejected on the way: a fixed number of reruns written as consecutive
  `if isMissingPartition` blocks (a loop with its count hidden), and
  reclassifying a post-heal miss as Transient so `DatastoreRetry` reruns
  it (its backoff delayed the rerun for no reason; a 100ms-ttl lab saw
  the delay as a partition aging past retention).
- Rejected: an unbounded heal-until-lands loop (no real bound); parsing
  the failing id out of the 23514 DETAIL text (Timescale's
  chunk-for-the-row shape, but a string contract with Postgres that
  breaks silently on a wording change, and the id it names is burned
  anyway).
