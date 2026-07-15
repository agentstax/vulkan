-- bindings: routing rules. A group with no binding matches all events; a
-- group WITH a binding only receives events whose routing_key matches
-- `pattern`, a NATS-wildcard pattern translated to a POSIX regex.
CREATE TABLE IF NOT EXISTS binding (
  id BIGSERIAL PRIMARY KEY,
  consumer_group TEXT NOT NULL,
  topic_id BIGINT NOT NULL,
  pattern TEXT NOT NULL,   -- POSIX regex translated from the NATS-style pattern
  display TEXT             -- original NATS pattern, for humans
);
CREATE INDEX IF NOT EXISTS binding_group ON binding (consumer_group, topic_id);
