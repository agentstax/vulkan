// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createTopicConfigSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS topic_config (
			id BIGSERIAL PRIMARY KEY,                                           -- corresponding id for table interpolation ie message_log_<id>
			system_id BIGINT NOT NULL REFERENCES system_config (id) ON DELETE CASCADE, -- owning system
			name TEXT NOT NULL UNIQUE,                                          -- user defined and displayed name
			partition_size BIGINT NOT NULL,                                     -- immutable after creation; message_log_<id>'s partition boundaries depend on it staying fixed
			retention_ttl_ns BIGINT NOT NULL DEFAULT 0,                         -- nanoseconds, time.Duration's own unit; 0 disables retention
			allow_drop_past_committed BOOLEAN NOT NULL DEFAULT false,           -- opt into Kafka's "lagging consumer falls off the retention window" semantics
			idempotency_key_ttl_ns BIGINT NOT NULL DEFAULT 86400000000000,      -- nanoseconds; unlike retention_ttl_ns, 0 isn't a supported "keep forever" value -- Config.SetDefaults never lets it reach 0, so the column default is the real 24h value, not 0
			delivery_log_mode TEXT NOT NULL DEFAULT 'failures',                 -- which outcomes write delivery_log_<id> rows
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`;
