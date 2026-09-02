// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createSystemConfigSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS %[1]s.system_config (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`;
