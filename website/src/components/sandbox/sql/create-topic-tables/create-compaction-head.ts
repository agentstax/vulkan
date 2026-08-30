// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { compactionHeadTable } from '../table-names';

export const createCompactionHeadSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			compaction_key  TEXT   NOT NULL PRIMARY KEY,
			head_id         BIGINT NOT NULL,            -- the winning message_log id for this key
			schema_version  BIGINT NOT NULL,            -- the winner's payload version; compared before rank
			compaction_rank BIGINT NOT NULL DEFAULT 0   -- the winner's rank
		);
	`;

export function createCompactionHeadSql(topicId: number): string {
	return interpolate(createCompactionHeadSqlTemplate, compactionHeadTable(topicId));
}
