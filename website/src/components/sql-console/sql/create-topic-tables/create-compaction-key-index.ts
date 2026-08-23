// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { messageLogTable } from '../table-names';

export const createCompactionKeyIndexSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %s_compaction_key ON %s (compaction_key, id)
			WHERE compaction_key IS NOT NULL;
	`;

export function createCompactionKeyIndexSql(topicId: number): string {
	return interpolate(createCompactionKeyIndexSqlTemplate, messageLogTable(topicId), messageLogTable(topicId));
}
