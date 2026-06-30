-- consumer for design 4 (claim-from-log, no per-event rows).
-- Pick a group, atomically reserve the next offset range by advancing its claim
-- frontier, READ that range from the log (the real work), then commit the
-- waterline. No deliveries rows on the happy path. Robust to an empty log.
-- Vars: :batch, :nshards, :ngroups.
\set g random(0, :ngroups - 1)
\set shard random(0, :nshards - 1)

-- reserve next range (lo, hi] for group g (commit 1); lo=-1 means no work.
-- LEAST caps the advance at head so we never claim offsets that don't exist yet;
-- old/new RETURNING gets the pre/post frontier atomically (concurrency-safe).
WITH h AS (SELECT COALESCE(max("offset"), 0) AS head FROM events),
upd AS (
  UPDATE cursors c SET claimed = LEAST(c.claimed + :batch, h.head)
  FROM h
  WHERE c.consumer_group = 'g' || :g AND c.claimed < h.head
  RETURNING old.claimed AS lo, new.claimed AS hi
)
SELECT COALESCE((SELECT lo FROM upd), -1) AS lo,
       COALESCE((SELECT hi FROM upd), -1) AS hi \gset

-- read the reserved range straight from the log. count(payload) (payload is NOT
-- indexed) forces a per-event HEAP fetch, modeling a real consumer reading each
-- event body -- NOT an index-only count over the offset PK.
SELECT COALESCE(count(payload), 0) AS got
  FROM events
 WHERE :lo >= 0 AND "offset" > :lo AND "offset" <= :hi \gset

-- commit waterline + completion counter (commit 2)
WITH adv AS (
  UPDATE cursors SET committed = GREATEST(committed, :hi)
   WHERE :lo >= 0 AND consumer_group = 'g' || :g
   RETURNING 1
)
UPDATE progress SET n = n + :got WHERE shard = :shard;
