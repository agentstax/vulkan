// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { bindingConfigLogTable } from '../table-names';

export const createBindingConfigLogIndexSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %s_group ON %s (consumer_group_id, status, declared_by, id);
	`;

export function createBindingConfigLogIndexSql(topicId: number): string {
	return interpolate(
		createBindingConfigLogIndexSqlTemplate,
		bindingConfigLogTable(topicId),
		bindingConfigLogTable(topicId),
	);
}
