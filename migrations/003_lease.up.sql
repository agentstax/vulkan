CREATE TABLE IF NOT EXISTS lease (
  token UUID NOT NULL DEFAULT gen_random_uuid(),
  consumer_group TEXT NOT NULL,
  topic_id BIGINT NOT NULL,        -- this is for range interpretation (which message_log_<id>)
  low BIGINT NOT NULL,             -- low of claimed range of lease
  high BIGINT NOT NULL,            -- high of claimed range of lease
  until TIMESTAMPTZ NOT NULL,      -- when the lease is considered expired and should be reclaimed
  reclaims INT NOT NULL DEFAULT 0, -- times this range has been reclaimed; past MaxReclaims it's quarantined
  PRIMARY KEY (token, consumer_group)
);