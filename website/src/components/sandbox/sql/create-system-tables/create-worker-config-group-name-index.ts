// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerConfigGroupNameIndexSql = `
			-- vulkan: system.createSystemTables
			CREATE UNIQUE INDEX IF NOT EXISTS worker_config_group_name ON %[1]s.worker_config (name, consumer_group_id) WHERE consumer_group_id IS NOT NULL;
		`;
