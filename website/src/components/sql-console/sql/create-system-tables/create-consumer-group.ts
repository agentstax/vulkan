// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createConsumerGroupSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS consumer_group (
			id BIGSERIAL PRIMARY KEY,                                         -- what children reference
			topic_id BIGINT NOT NULL REFERENCES topic (id) ON DELETE CASCADE, -- owning topic
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (topic_id, name)
		);
	`;
