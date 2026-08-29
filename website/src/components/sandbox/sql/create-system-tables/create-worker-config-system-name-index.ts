// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerConfigSystemNameIndexSql = `
			-- vulkan: system.createSystemTables
			CREATE UNIQUE INDEX IF NOT EXISTS worker_config_system_name ON worker_config (name, system_id) WHERE system_id IS NOT NULL;
		`;
