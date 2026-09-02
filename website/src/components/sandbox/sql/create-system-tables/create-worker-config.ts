// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerConfigSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS %[1]s.worker_config (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES %[1]s.system_config (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES %[1]s.topic_config (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES %[1]s.consumer_group_config (id) ON DELETE CASCADE,
			name TEXT NOT NULL,                      -- 'janitor' | 'cursor_advancer' | 'schedule_producer' | user-defined
			metadata JSONB NOT NULL DEFAULT '{}',    -- per-worker config, written by the declaration that creates the row
			target_instances INT NOT NULL DEFAULT 1, -- 0 = suspended, -1 = unbounded
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1),
			CHECK (target_instances >= -1)
		);
	`;
