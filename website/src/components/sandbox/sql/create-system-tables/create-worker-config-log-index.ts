// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerLogIndexSql = `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS worker_log_worker ON worker_log (worker_id, id);
	`;
