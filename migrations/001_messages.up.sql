-- append only log of messages
CREATE TABLE IF NOT EXISTS message_log (
  id BIGSERIAL PRIMARY KEY,
  payload JSONB,
  created_at TIMESTAMPTZ DEFAULT NOW()
);