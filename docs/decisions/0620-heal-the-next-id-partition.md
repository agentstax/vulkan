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
- A miss the heal has just covered is returned as `errPartitionMissing`
  (VK0056, Transient), so the attempt reruns on `DatastoreRetry`'s
  schedule -- the existing bound -- instead of a hand-rolled single
  retry. `AppendMessageInTx` keeps one heal + one rerun: the caller
  owns the transaction and its retry.
- The heal warn line is declared (VK0057, `eventPartitionCreatedOnInsert`)
  beside VK0033 and carries `message_id`.

## Consequences

- A straddling batch heals twice and lands; the lab's premise "cap <=
  PartitionSize so one heal covers a whole batch" is deleted.
- The batch path no longer wraps its heal in a retry of its own: a heal
  lock timeout fails the produce fast on every path, as VK0018's note
  already said for the single-row paths.
- Rejected: an unbounded heal-until-lands loop (no real bound); parsing
  the failing id out of the 23514 DETAIL text (Timescale's
  chunk-for-the-row shape, but a string contract with Postgres that
  breaks silently on a wording change, and the id it names is burned
  anyway).
