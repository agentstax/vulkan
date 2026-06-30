-- producer: append :pbatch events per transaction (pbatch=1 => single-row insert).
-- Fires whatever AFTER INSERT trigger is installed (d1 fanout).
\set pbatch :pbatch
INSERT INTO events(payload) SELECT '{"x":1}'::jsonb FROM generate_series(1, :pbatch);
