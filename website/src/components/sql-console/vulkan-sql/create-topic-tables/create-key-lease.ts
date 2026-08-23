// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { keyLeaseTable } from '../table-names';

export const createKeyLeaseSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group_id BIGINT NOT NULL, -- PK
			compaction_key TEXT NOT NULL,      -- PK
			lease_token UUID NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (consumer_group_id, compaction_key)
		);
	`;

export function createKeyLeaseSql(topicId: number): string {
	return interpolate(createKeyLeaseSqlTemplate, keyLeaseTable(topicId));
}
