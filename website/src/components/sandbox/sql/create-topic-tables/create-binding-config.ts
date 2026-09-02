// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { bindingConfigTable } from '../table-names';

export const createBindingConfigSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %[1]s.%[2]s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES %[1]s.consumer_group_config (id) ON DELETE CASCADE,
			pattern_regex TEXT NOT NULL,              -- POSIX regex translated from the declared pattern
			pattern TEXT,                             -- the declared NATS-style pattern, for humans
			UNIQUE (consumer_group_id, pattern_regex) -- its index also serves the group lookup
		);
	`;

export function createBindingConfigSql(topicId: number): string {
	return interpolate(createBindingConfigSqlTemplate, bindingConfigTable(topicId));
}
