// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { idempotencyKeyTable } from '../table-names';

export const createIdempotencyKeySqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %[1]s.%[2]s (
			idempotency_key UUID NOT NULL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`;

export function createIdempotencyKeySql(topicId: number): string {
	return interpolate(createIdempotencyKeySqlTemplate, idempotencyKeyTable(topicId));
}
