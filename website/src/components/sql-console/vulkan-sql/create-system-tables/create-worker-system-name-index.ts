// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerSystemNameIndexSql = `-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_system_name ON worker (name, system_id) WHERE system_id IS NOT NULL;`;
