CREATE TABLE IF NOT EXISTS topic (
  id BIGSERIAL PRIMARY KEY,                                      -- corresponding id for table interpolation ie message_log_<id>
  name TEXT UNIQUE NOT NULL,                                     -- user defined and displayed name
  partition_size BIGINT NOT NULL,                                -- immutable after creation; message_log_<id>'s partition boundaries depend on it staying fixed
  retention_ttl_ns BIGINT NOT NULL DEFAULT 0,                    -- nanoseconds, time.Duration's own unit; 0 disables retention
  allow_drop_past_committed BOOLEAN NOT NULL DEFAULT false,      -- opt into Kafka's "lagging consumer falls off the retention window" semantics
  idempotency_key_ttl_ns BIGINT NOT NULL DEFAULT 86400000000000, -- nanoseconds; unlike retention_ttl_ns, 0 isn't a supported "keep forever" value -- Config.SetDefaults never lets it reach 0, so the column default is the real 24h value, not 0
  disable_delivery_log BOOLEAN NOT NULL DEFAULT false,           -- opt out of delivery_log_<id> (per-attempt failure audit trail)
  janitor_poll_rate_ns BIGINT NOT NULL DEFAULT 5000000000,       -- nanoseconds; how often the janitor loop ticks (create-ahead, drop/sweep expired partitions, sweep idempotency_key)
  janitor_sweep_batch_size INT NOT NULL DEFAULT 1000,            -- rows deleted per sweep transaction; caps how much of a backlog one batch holds a lock for
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
