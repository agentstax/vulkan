-- pgbench custom script: single-row insert into events.
-- Fires whatever AFTER INSERT trigger variant is currently installed.
INSERT INTO events(payload) VALUES ('{"x":1}'::jsonb);
