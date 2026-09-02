// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerInstanceWorkerIndexSql = `
			-- vulkan: system.createSystemTables
			CREATE INDEX IF NOT EXISTS worker_instance_worker ON %[1]s.worker_instance (worker_id);
		`;
