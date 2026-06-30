-- Executable harness for the hybrid waterline v2 design, to adversarially TEST correctness.
-- Each op is an atomic function (its own txn) so we can craft interleavings from separate sessions.
-- A `processed` ledger records every (group,offset) handled so we can detect gaps / double-process.
DROP TABLE IF EXISTS events, cursors, leases, deliveries, processed CASCADE;
DROP TYPE IF EXISTS delivery_state CASCADE;

CREATE TABLE events ("offset" BIGSERIAL PRIMARY KEY, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE cursors (consumer_group TEXT NOT NULL, lane INT NOT NULL DEFAULT 0,
  committed BIGINT NOT NULL DEFAULT 0, claimed BIGINT NOT NULL DEFAULT 0, PRIMARY KEY (consumer_group,lane));
CREATE TABLE leases (consumer_group TEXT NOT NULL, lane INT NOT NULL, lo BIGINT NOT NULL, hi BIGINT NOT NULL,
  lease_until TIMESTAMPTZ NOT NULL, lease_token UUID NOT NULL, PRIMARY KEY (consumer_group,lane,lo));
CREATE TYPE delivery_state AS ENUM ('ready','inflight','dead');
CREATE TABLE deliveries (consumer_group TEXT NOT NULL, "offset" BIGINT NOT NULL,
  state delivery_state NOT NULL DEFAULT 'ready', attempts INT NOT NULL DEFAULT 0,
  available_at TIMESTAMPTZ NOT NULL DEFAULT now(), lease_until TIMESTAMPTZ, lease_token UUID, last_error TEXT,
  PRIMARY KEY (consumer_group,"offset"));
CREATE INDEX deliveries_ready ON deliveries (consumer_group,"offset") WHERE state='ready';
-- ledger: every time any path "processes" an offset we bump times; detects gaps (0) & double-process (>1 w/o crash)
CREATE TABLE processed (consumer_group TEXT NOT NULL, "offset" BIGINT NOT NULL, times INT NOT NULL DEFAULT 0,
  PRIMARY KEY (consumer_group,"offset"));

-- record processing of one offset (idempotent target = the ledger row, not the work)
CREATE OR REPLACE FUNCTION wl_process(g TEXT, off BIGINT) RETURNS void AS $$
  INSERT INTO processed(consumer_group,"offset",times) VALUES (g,off,1)
  ON CONFLICT (consumer_group,"offset") DO UPDATE SET times = processed.times + 1;
$$ LANGUAGE sql;

-- happy-path Claim: advance the read frontier on a lane, write a lease, return (lo,hi,token). Atomic.
CREATE OR REPLACE FUNCTION wl_claim(g TEXT, ln INT, batch INT, lease_secs INT)
RETURNS TABLE(lo BIGINT, hi BIGINT, token UUID) AS $$
DECLARE h BIGINT; o BIGINT; n BIGINT; t UUID;
BEGIN
  SELECT COALESCE(max("offset"),0) INTO h FROM events;
  UPDATE cursors c SET claimed = LEAST(c.claimed+batch, h)
    WHERE c.consumer_group=g AND c.lane=ln AND c.claimed < h
    RETURNING old.claimed, new.claimed INTO o, n;          -- PG18 old/new RETURNING
  IF NOT FOUND THEN RETURN; END IF;
  t := gen_random_uuid();
  INSERT INTO leases(consumer_group,lane,lo,hi,lease_until,lease_token)
    VALUES (g,ln,o,n, now()+make_interval(secs=>lease_secs), t);
  RETURN QUERY SELECT o,n,t;
END $$ LANGUAGE plpgsql;

-- Reclaim ONE expired lease (FOR UPDATE SKIP LOCKED), refresh token+deadline, return its range. Atomic.
CREATE OR REPLACE FUNCTION wl_reclaim(g TEXT, lease_secs INT)
RETURNS TABLE(lane INT, lo BIGINT, hi BIGINT, token UUID) AS $$
DECLARE r RECORD; t UUID;
BEGIN
  SELECT l.lane AS lane, l.lo AS lo, l.hi AS hi INTO r
    FROM leases l WHERE l.consumer_group=g AND l.lease_until < now()
    ORDER BY l.lo FOR UPDATE SKIP LOCKED LIMIT 1;
  IF NOT FOUND THEN RETURN; END IF;
  t := gen_random_uuid();
  UPDATE leases SET lease_until=now()+make_interval(secs=>lease_secs), lease_token=t
    WHERE leases.consumer_group=g AND leases.lane=r.lane AND leases.lo=r.lo;
  RETURN QUERY SELECT r.lane, r.lo, r.hi, t;
END $$ LANGUAGE plpgsql;

-- Commit a range. SAFE semantics: free the lease FIRST (token-guarded); only if we still owned it do we
-- park the failures. If the lease was stolen/expired-reclaimed (token mismatch), this is a no-op => the
-- reclaimer owns the range. Returns true iff we committed.
CREATE OR REPLACE FUNCTION wl_commit(g TEXT, ln INT, lo_ BIGINT, tok UUID, exc BIGINT[])
RETURNS BOOLEAN AS $$
DECLARE freed INT;
BEGIN
  DELETE FROM leases WHERE consumer_group=g AND lane=ln AND lo=lo_ AND lease_token=tok;
  GET DIAGNOSTICS freed = ROW_COUNT;
  IF freed = 0 THEN RETURN FALSE; END IF;                  -- lost the lease; reclaimer owns it
  IF array_length(exc,1) IS NOT NULL THEN
    INSERT INTO deliveries(consumer_group,"offset",state,attempts,available_at,last_error)
      SELECT g, o, 'ready', 1, now()+interval '5 seconds', 'first-pass fail'
      FROM unnest(exc) o ON CONFLICT (consumer_group,"offset") DO NOTHING;
  END IF;
  RETURN TRUE;
END $$ LANGUAGE plpgsql;

-- DOC-AS-WRITTEN (unsafe) variant: park THEN free, ungated. Used to demonstrate the double-park hazard.
CREATE OR REPLACE FUNCTION wl_commit_doc(g TEXT, ln INT, lo_ BIGINT, tok UUID, exc BIGINT[])
RETURNS BOOLEAN AS $$
DECLARE freed INT;
BEGIN
  IF array_length(exc,1) IS NOT NULL THEN
    INSERT INTO deliveries(consumer_group,"offset",state,attempts,available_at,last_error)
      SELECT g, o, 'ready', 1, now()+interval '5 seconds', 'first-pass fail'
      FROM unnest(exc) o ON CONFLICT (consumer_group,"offset") DO NOTHING;
  END IF;
  DELETE FROM leases WHERE consumer_group=g AND lane=ln AND lo=lo_ AND lease_token=tok;
  GET DIAGNOSTICS freed = ROW_COUNT;
  RETURN freed > 0;
END $$ LANGUAGE plpgsql;

-- Advance the lane waterline to the lowest blocker (open lease lo / unresolved exception offset-1), else claimed.
CREATE OR REPLACE FUNCTION wl_advance(g TEXT, ln INT) RETURNS BIGINT AS $$
DECLARE c BIGINT;
BEGIN
  UPDATE cursors cu SET committed = GREATEST(cu.committed, COALESCE(
      LEAST(
        (SELECT min(lo) FROM leases WHERE consumer_group=g AND lane=ln),
        (SELECT min("offset")-1 FROM deliveries WHERE consumer_group=g AND state IN ('ready','inflight'))
      ), cu.claimed))
    WHERE cu.consumer_group=g AND cu.lane=ln RETURNING committed INTO c;
  RETURN c;
END $$ LANGUAGE plpgsql;

-- variant of Advance that filters the exception blocker by LANE too (fix candidate for the sharded bug)
CREATE OR REPLACE FUNCTION wl_advance_lanefix(g TEXT, ln INT, k INT) RETURNS BIGINT AS $$
DECLARE c BIGINT;
BEGIN
  UPDATE cursors cu SET committed = GREATEST(cu.committed, COALESCE(
      LEAST(
        (SELECT min(lo) FROM leases WHERE consumer_group=g AND lane=ln),
        (SELECT min("offset")-1 FROM deliveries WHERE consumer_group=g AND state IN ('ready','inflight')
           AND ("offset" % k) = ln)                          -- only THIS lane's stripe blocks it
      ), cu.claimed))
    WHERE cu.consumer_group=g AND cu.lane=ln RETURNING committed INTO c;
  RETURN c;
END $$ LANGUAGE plpgsql;

-- exception drain: claim ready -> inflight (SKIP LOCKED); ack=DELETE; nack-> ready/backoff or dead at maxatt.
CREATE OR REPLACE FUNCTION wl_claim_exc(g TEXT, n INT, lease_secs INT)
RETURNS TABLE("offset" BIGINT, token UUID) AS $$
DECLARE t UUID := gen_random_uuid();
BEGIN
  RETURN QUERY
  UPDATE deliveries d SET state='inflight', lease_token=t, lease_until=now()+make_interval(secs=>lease_secs),
                          attempts=attempts+1
   WHERE (d.consumer_group,d."offset") IN (
     SELECT dd.consumer_group, dd."offset" FROM deliveries dd
      WHERE dd.consumer_group=g AND dd.state='ready' AND dd.available_at<=now()
      ORDER BY dd."offset" FOR UPDATE SKIP LOCKED LIMIT n)
   RETURNING d."offset", t;
END $$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION wl_ack_exc(g TEXT, off BIGINT, tok UUID) RETURNS BOOLEAN AS $$
DECLARE r INT; BEGIN
  DELETE FROM deliveries WHERE consumer_group=g AND "offset"=off AND lease_token=tok;
  GET DIAGNOSTICS r=ROW_COUNT; RETURN r>0; END $$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION wl_nack_exc(g TEXT, off BIGINT, tok UUID, maxatt INT, backoff_secs INT) RETURNS TEXT AS $$
DECLARE st delivery_state; a INT; BEGIN
  SELECT attempts INTO a FROM deliveries WHERE consumer_group=g AND "offset"=off AND lease_token=tok;
  IF NOT FOUND THEN RETURN 'lost'; END IF;
  IF a >= maxatt THEN st='dead'; ELSE st='ready'; END IF;
  UPDATE deliveries SET state=st, lease_token=NULL, lease_until=NULL,
         available_at = now()+make_interval(secs=>backoff_secs)
   WHERE consumer_group=g AND "offset"=off; RETURN st::text; END $$ LANGUAGE plpgsql;

-- seed helpers
CREATE OR REPLACE FUNCTION wl_seed(g TEXT, lanes INT, nevents BIGINT) RETURNS void AS $$
BEGIN
  INSERT INTO events(payload) SELECT '{"x":1}'::jsonb FROM generate_series(1,nevents);
  INSERT INTO cursors(consumer_group,lane) SELECT g, s FROM generate_series(0,lanes-1) s
    ON CONFLICT DO NOTHING;
END $$ LANGUAGE plpgsql;
