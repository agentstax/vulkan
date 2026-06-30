-- consumer for materialize designs (d1/d2/d3): two-phase claim+ack on deliveries.
-- One pgbench "transaction" = claim a batch (ready->inflight, tagged) then ack it
-- (inflight->acked) as TWO separate commits (fsync-honest happy path).
-- Vars: :batch, :nshards.
\set tok :client_id * 100000000 + random(1, 99999999)
\set shard random(0, :nshards - 1)

-- claim: lowest ready rows -> inflight, tagged with our token
WITH c AS (
  SELECT consumer_group, "offset"
  FROM deliveries
  WHERE state = 'ready'
  ORDER BY "offset"
  FOR UPDATE SKIP LOCKED
  LIMIT :batch
)
UPDATE deliveries d
   SET state = 'inflight', lease_token = :tok,
       lease_until = now() + interval '30 seconds', attempts = attempts + 1
  FROM c
 WHERE d.consumer_group = c.consumer_group AND d."offset" = c."offset";

-- ack: our tagged inflight rows -> acked; bump completion counter by how many
WITH a AS (
  UPDATE deliveries SET state = 'acked'
   WHERE lease_token = :tok AND state = 'inflight'
   RETURNING 1
)
UPDATE progress SET n = n + (SELECT count(*) FROM a) WHERE shard = :shard;
