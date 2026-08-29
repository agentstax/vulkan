// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createCronJobCursorDueIndexSql = `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS cron_job_cursor_due ON cron_job_cursor (next_scheduled_at);
	`;
