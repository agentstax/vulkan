// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { exceptionQueueTable } from '../table-names';

export const createExceptionQueueMessageKeyIndexSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %[2]s_message_key ON %[1]s.%[3]s (consumer_group_id, message_key, message_id);
	`;

export function createExceptionQueueMessageKeyIndexSql(topicId: number): string {
	return interpolate(
		createExceptionQueueMessageKeyIndexSqlTemplate,
		exceptionQueueTable(topicId),
		exceptionQueueTable(topicId),
	);
}
