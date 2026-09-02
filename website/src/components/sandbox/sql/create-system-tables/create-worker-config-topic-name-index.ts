// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerConfigTopicNameIndexSql = `
			-- vulkan: system.createSystemTables
			CREATE UNIQUE INDEX IF NOT EXISTS worker_config_topic_name ON %[1]s.worker_config (name, topic_id) WHERE topic_id IS NOT NULL;
		`;
