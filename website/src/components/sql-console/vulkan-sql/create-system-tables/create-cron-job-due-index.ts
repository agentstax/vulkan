// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createCronJobDueIndexSql = `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS cron_job_due ON cron_job (next_scheduled_time) WHERE NOT suspended;
	`;
