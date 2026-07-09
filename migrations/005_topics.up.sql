CREATE TABLE IF NOT EXISTS topics (
  id BIGSERIAL PRIMARY KEY, -- corresponding id for table interpolation ie message_log_<id>
  name TEXT UNIQUE NOT NULL, -- user defined and displayed name
  partition_size BIGINT NOT NULL, -- immutable after creation; message_log_<id>'s partition boundaries depend on it staying fixed
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
