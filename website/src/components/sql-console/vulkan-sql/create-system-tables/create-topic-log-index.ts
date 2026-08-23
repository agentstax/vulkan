// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createTopicLogIndexSql = `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS topic_log_topic ON topic_log (topic_id, id);
	`;
