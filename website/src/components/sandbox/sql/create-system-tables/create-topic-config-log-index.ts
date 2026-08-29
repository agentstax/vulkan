// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createTopicConfigLogIndexSql = `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS topic_config_log_topic ON topic_config_log (topic_id, id);
	`;
