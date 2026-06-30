-- SHARDED-FRONTIER claim-from-log (audit E2): lift the single-frontier g1 wall.
-- Each group g is split into :kfront frontier shards 'g{g}#{s}', each owning a
-- DISJOINT contiguous block of the offset space. A consumer txn picks a random
-- (group, shard), advances that shard's claim frontier within its block cap
-- (stored in cursors.projected), reads its sub-range, commits the waterline.
-- Disjoint blocks => no overlap, no double-process, union = full log exactly once.
-- Spreading clients across kfront cursor rows removes the single-row contention.
-- Vars: :batch, :nshards, :ngroups, :kfront.
\set g random(0, :ngroups - 1)
\set s random(0, :kfront - 1)
\set shard random(0, :nshards - 1)

-- advance this shard's frontier, capped at its block end (cursors.projected)
WITH upd AS (
  UPDATE cursors c SET claimed = LEAST(c.claimed + :batch, c.projected)
  WHERE c.consumer_group = 'g' || :g || '#' || :s AND c.claimed < c.projected
  RETURNING old.claimed AS lo, new.claimed AS hi
)
SELECT COALESCE((SELECT lo FROM upd), -1) AS lo,
       COALESCE((SELECT hi FROM upd), -1) AS hi \gset

-- read the reserved contiguous sub-range straight from the log (heap fetch)
SELECT COALESCE(count(payload), 0) AS got
  FROM events
 WHERE :lo >= 0 AND "offset" > :lo AND "offset" <= :hi \gset

WITH adv AS (
  UPDATE cursors SET committed = GREATEST(committed, :hi)
   WHERE :lo >= 0 AND consumer_group = 'g' || :g || '#' || :s
   RETURNING 1
)
UPDATE progress SET n = n + :got WHERE shard = :shard;
