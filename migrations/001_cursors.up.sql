-- consumer group cursors for tracking offset in message_log
CREATE TABLE IF NOT EXISTS cursors (
  consumer_group TEXT NOT NULL,
  topic_id BIGINT NOT NULL, -- a group tracks an independent cursor per topic
  claimed BIGINT NOT NULL DEFAULT 0, -- the read frontier 'inflight' work
  committed BIGINT NOT NULL DEFAULT 0, -- every message id > committed is in an end state done / dead
  PRIMARY KEY (consumer_group, topic_id)
);