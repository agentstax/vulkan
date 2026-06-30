-- consumer for design 5 (range-lease / cumulative-commit).
-- Like claim-from-log but records a durable lease row per reserved range (so a
-- crashed range is reclaimable), then on commit deletes the lease, advances the
-- waterline, and bumps the counter. Cost vs d4 = +1 INSERT and +1 DELETE / batch.
-- Vars: :batch, :nshards, :ngroups.
\set g random(0, :ngroups - 1)
\set shard random(0, :nshards - 1)
\set tok :client_id * 100000000 + random(1, 99999999)

-- reserve next range AND record the lease (commit 1)
WITH h AS (SELECT COALESCE(max("offset"), 0) AS head FROM events),
upd AS (
  UPDATE cursors c SET claimed = LEAST(c.claimed + :batch, h.head)
  FROM h
  WHERE c.consumer_group = 'g' || :g AND c.claimed < h.head
  RETURNING old.claimed AS lo, new.claimed AS hi
),
ins AS (
  INSERT INTO leases(consumer_group, lo, hi, lease_until, lease_token)
  SELECT 'g' || :g, lo, hi, now() + interval '30 seconds', :tok FROM upd
  ON CONFLICT DO NOTHING
  RETURNING lo, hi
)
SELECT COALESCE((SELECT lo FROM ins), -1) AS lo,
       COALESCE((SELECT hi FROM ins), -1) AS hi \gset

-- read the reserved range from the log; count(payload) forces a per-event HEAP
-- fetch (payload is unindexed), modeling a real consumer reading each event body.
SELECT COALESCE(count(payload), 0) AS got
  FROM events
 WHERE :lo >= 0 AND "offset" > :lo AND "offset" <= :hi \gset

-- commit: delete the lease, advance waterline, bump counter (commit 2)
WITH d AS (
  DELETE FROM leases WHERE :lo >= 0 AND consumer_group = 'g' || :g AND lo = :lo
  RETURNING 1
),
adv AS (
  UPDATE cursors SET committed = GREATEST(committed, :hi)
   WHERE :lo >= 0 AND consumer_group = 'g' || :g
   RETURNING 1
)
UPDATE progress SET n = n + :got WHERE shard = :shard;
