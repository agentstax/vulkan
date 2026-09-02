// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createMigrationLogSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS %[1]s.migration_log (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES %[1]s.system_config (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES %[1]s.topic_config (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES %[1]s.consumer_group_config (id) ON DELETE CASCADE,
			migration_version BIGINT NOT NULL,
			min_compatible_version BIGINT NOT NULL DEFAULT 0, -- the step's MinCompatibleVersion; 0 on baseline and down rows
			status TEXT NOT NULL,                             -- 'success' | 'failure' (extensible)
			error TEXT,                                       -- populated when status = 'failure'
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1)
		);
	`;
