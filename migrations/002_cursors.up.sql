-- consumer group cursors for tracking offset in message_log
CREATE TABLE IF NOT EXISTS cursors (
  consumer_group TEXT NOT NULL PRIMARY KEY,
  claimed BIGINT NOT NULL DEFAULT 0, -- the read frontier 'inflight' work
  committed BIGINT NOT NULL DEFAULT 0 -- every message id > committed is in an end state done / dead
);