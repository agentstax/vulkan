CREATE TABLE IF NOT EXISTS leases (
  token UUID NOT NULL DEFAULT gen_random_uuid(),
  consumer_group TEXT NOT NULL,
  low BIGINT NOT NULL, -- low of claimed range of lease
  high BIGINT NOT NULL, -- high of claimed range of lease
  until TIMESTAMPTZ NOT NULL, -- when the lease is considered expired and should be reclaimed
  PRIMARY KEY (token, consumer_group)
);