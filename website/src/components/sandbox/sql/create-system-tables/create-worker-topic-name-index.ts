// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerTopicNameIndexSql = `-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_topic_name ON worker (name, topic_id) WHERE topic_id IS NOT NULL;`;
