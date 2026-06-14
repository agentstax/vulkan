-- testing table for transactional enqueues
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT uuidv7(),
  username TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);