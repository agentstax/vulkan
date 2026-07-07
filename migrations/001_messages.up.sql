-- append only log of messages
CREATE TABLE IF NOT EXISTS message_log (
  id BIGSERIAL PRIMARY KEY,
  routing_key TEXT, -- optional
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (id); -- we partition by range and not date b/c all hot path queries filter on id not date

-- first partition
CREATE TABLE IF NOT EXISTS message_log_0
  PARTITION OF message_log
  FOR VALUES FROM (0) TO (1000000); -- TODO once convert migration scripts to code need to make size config driven with sane default

