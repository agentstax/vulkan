-- bindings: routing rules. A group with no binding matches all events; a
-- group WITH a binding only receives events whose routing_key matches
-- `pattern`, a NATS-wildcard pattern translated to a POSIX regex.
CREATE TABLE IF NOT EXISTS bindings (
  id BIGSERIAL PRIMARY KEY,
  consumer_group TEXT NOT NULL,
  pattern TEXT NOT NULL,   -- POSIX regex translated from the NATS-style pattern
  display TEXT             -- original NATS pattern, for humans
);
CREATE INDEX IF NOT EXISTS bindings_group ON bindings (consumer_group);
