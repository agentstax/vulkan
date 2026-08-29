// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createCronJobCursorSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS cron_job_cursor (
			cron_job_id BIGINT NOT NULL PRIMARY KEY REFERENCES cron_job_config (id) ON DELETE CASCADE,
			next_scheduled_at TIMESTAMPTZ NOT NULL,
			last_scheduled_at TIMESTAMPTZ               -- the scheduled time most recently produced
		);
	`;
