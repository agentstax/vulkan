-- append only log of messages
CREATE TABLE IF NOT EXISTS message_log (
  id BIGSERIAL PRIMARY KEY,
  routing_key TEXT, -- optional
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);