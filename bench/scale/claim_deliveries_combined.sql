-- consumer for materialize designs, COMBINED-COMMIT variant: claim+ack in ONE
-- transaction (ready->acked directly, no separate inflight lease commit). Halves
-- the fsync count vs claim_deliveries.sql at the cost of the in-flight lease
-- (at-most-once on crash). Used to measure the commit-count optimization lever.
-- Vars: :batch, :nshards.
\set shard random(0, :nshards - 1)
WITH c AS (
  SELECT consumer_group, "offset"
  FROM deliveries
  WHERE state = 'ready'
  ORDER BY "offset"
  FOR UPDATE SKIP LOCKED
  LIMIT :batch
), a AS (
  UPDATE deliveries d SET state = 'acked', attempts = attempts + 1
  FROM c
  WHERE d.consumer_group = c.consumer_group AND d."offset" = c."offset"
  RETURNING 1
)
UPDATE progress SET n = n + (SELECT count(*) FROM a) WHERE shard = :shard;
