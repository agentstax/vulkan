-- POP-DELETE per-row consumer (audit E3): the classic fast queue pattern.
-- ONE transaction: SELECT the lowest ready rows FOR UPDATE SKIP LOCKED, DELETE
-- them, bump the counter. Replaces the baseline's two state-UPDATEs (ready->inflight
-- ->acked, i.e. 2 new heap row-versions + index churn per row) with a single DELETE
-- per row. At-most-once on crash (no inflight lease), but it is the cheapest possible
-- per-row write path. Tests whether the ~124k wall is dead-tuple/UPDATE-bloat bound.
-- Vars: :batch, :nshards.
\set shard random(0, :nshards - 1)
WITH c AS (
  SELECT consumer_group, "offset"
  FROM deliveries
  WHERE state = 'ready'
  ORDER BY "offset"
  FOR UPDATE SKIP LOCKED
  LIMIT :batch
), d AS (
  DELETE FROM deliveries del
   USING c
   WHERE del.consumer_group = c.consumer_group AND del."offset" = c."offset"
   RETURNING 1
)
UPDATE progress SET n = n + (SELECT count(*) FROM d) WHERE shard = :shard;
