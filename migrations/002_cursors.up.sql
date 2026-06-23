-- consumer group cursors for tracking offset in message_log
CREATE TABLE IF NOT EXISTS cursors (
  consumer_group TEXT NOT NULL PRIMARY KEY,
  position BIGINT NOT NULL DEFAULT 0 -- where in the message_log the consumer group is at
);