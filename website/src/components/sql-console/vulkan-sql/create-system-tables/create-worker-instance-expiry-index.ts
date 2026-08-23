// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerInstanceExpiryIndexSql = `
			-- vulkan: system.createSystemTables
			CREATE INDEX IF NOT EXISTS worker_instance_expiry ON worker_instance (expires_at);
		`;
