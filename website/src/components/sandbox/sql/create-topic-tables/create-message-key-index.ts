// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { messageLogTable } from '../table-names';

export const createMessageKeyIndexSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %[2]s_message_key ON %[1]s.%[3]s (message_key, id)
			WHERE message_key IS NOT NULL;
	`;

export function createMessageKeyIndexSql(topicId: number): string {
	return interpolate(
		createMessageKeyIndexSqlTemplate,
		messageLogTable(topicId),
		messageLogTable(topicId),
	);
}
