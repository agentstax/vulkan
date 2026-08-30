// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createScheduleCursorSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS schedule_cursor (
			schedule_id BIGINT NOT NULL PRIMARY KEY REFERENCES schedule_config (id) ON DELETE CASCADE,
			next_scheduled_at TIMESTAMPTZ NOT NULL,
			last_scheduled_at TIMESTAMPTZ               -- the scheduled time most recently produced
		);
	`;
