// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerGroupNameIndexSql = `-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_group_name ON worker (name, consumer_group_id) WHERE consumer_group_id IS NOT NULL;`;
