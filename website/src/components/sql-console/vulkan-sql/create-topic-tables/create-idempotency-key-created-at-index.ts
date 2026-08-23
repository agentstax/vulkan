// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { idempotencyKeyTable } from '../table-names';

export const createIdempotencyKeyCreatedAtIndexSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %s_created_at ON %s (created_at);
	`;

export function createIdempotencyKeyCreatedAtIndexSql(topicId: number): string {
	return interpolate(createIdempotencyKeyCreatedAtIndexSqlTemplate, idempotencyKeyTable(topicId), idempotencyKeyTable(topicId));
}
