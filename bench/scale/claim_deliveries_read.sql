-- READ-PARITY variant of claim_deliveries.sql (audit E1/A1).
-- Identical claim+ack as the baseline per-row consumer, BUT inserts a per-batch
-- HEAP read of the claimed events' payloads between claim and ack -- modeling a
-- real consumer that must read each event body to do work. This makes the per-row
-- path apples-to-apples with claim-from-log (which reads count(payload)).
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

-- READ the event payloads for the rows we just claimed (the real consumer work).
-- count(payload) forces a per-event heap fetch (payload is unindexed).
SELECT COALESCE(count(e.payload), 0) AS got
  FROM events e
 WHERE e."offset" IN (
   SELECT d."offset" FROM deliveries d
    WHERE d.lease_token = :tok AND d.state = 'inflight'
 );

-- ack: our tagged inflight rows -> acked; bump completion counter by how many
WITH a AS (
  UPDATE deliveries SET state = 'acked'
   WHERE lease_token = :tok AND state = 'inflight'
   RETURNING 1
)
UPDATE progress SET n = n + (SELECT count(*) FROM a) WHERE shard = :shard;
