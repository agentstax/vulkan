// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createTopicConfigLogSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS %[1]s.topic_config_log (
			id BIGSERIAL PRIMARY KEY,
			topic_id BIGINT NOT NULL REFERENCES %[1]s.topic_config (id) ON DELETE CASCADE,
			name TEXT NOT NULL,                          -- the topic's name as of this declaration
			partition_size BIGINT NOT NULL,
			retention_ttl_ns BIGINT NOT NULL,
			allow_drop_past_committed BOOLEAN NOT NULL,
			idempotency_key_ttl_ns BIGINT NOT NULL,
			delivery_log_mode TEXT NOT NULL,
			declared_by TEXT NOT NULL,                   -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`;
