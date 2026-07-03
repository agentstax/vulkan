-- deliveries(consumer_group text, event_offset bigint, status, attempts, locked_at, last_error, PRIMARY KEY(consumer_group, event_offset))
CREATE TABLE IF NOT EXISTS deliveries (
  consumer_group TEXT NOT NULL, -- PK
  message_id BIGINT NOT NULL,   -- PK
  status TEXT NOT NULL,
  attempts INT NOT NULL default 0,
  lease_until TIMESTAMPTZ,
  lease_token UUID,
  can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- backoff between retries
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (consumer_group, message_id)
);