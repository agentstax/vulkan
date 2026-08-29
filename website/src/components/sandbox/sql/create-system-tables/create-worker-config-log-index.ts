// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerConfigLogIndexSql = `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS worker_config_log_worker ON worker_config_log (worker_id, id);
	`;
