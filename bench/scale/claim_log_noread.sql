-- NO-READ variant of claim_log.sql (audit E1/A3): identical range reservation and
-- waterline commit, but the "read" is count(*) instead of count(payload). On a
-- VACUUMed events table this is an Index-Only Scan (0 heap fetches) -- i.e. pure
-- bookkeeping with no event-body read. Used to isolate the cost of the heap read.
-- Vars: :batch, :nshards, :ngroups.
\set g random(0, :ngroups - 1)
\set shard random(0, :nshards - 1)

WITH h AS (SELECT COALESCE(max("offset"), 0) AS head FROM events),
upd AS (
  UPDATE cursors c SET claimed = LEAST(c.claimed + :batch, h.head)
  FROM h
  WHERE c.consumer_group = 'g' || :g AND c.claimed < h.head
  RETURNING old.claimed AS lo, new.claimed AS hi
)
SELECT COALESCE((SELECT lo FROM upd), -1) AS lo,
       COALESCE((SELECT hi FROM upd), -1) AS hi \gset

-- count(*) over the PK range = Index-Only Scan when VM is all-visible (no heap read)
SELECT COALESCE(count(*), 0) AS got
  FROM events
 WHERE :lo >= 0 AND "offset" > :lo AND "offset" <= :hi \gset

WITH adv AS (
  UPDATE cursors SET committed = GREATEST(committed, :hi)
   WHERE :lo >= 0 AND consumer_group = 'g' || :g
   RETURNING 1
)
UPDATE progress SET n = n + :got WHERE shard = :shard;
