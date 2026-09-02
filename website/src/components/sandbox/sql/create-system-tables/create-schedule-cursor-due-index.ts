// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createScheduleCursorDueIndexSql = `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS schedule_cursor_due ON %[1]s.schedule_cursor (next_scheduled_at);
	`;
