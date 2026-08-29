// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { messageKeyLeaseTable } from '../table-names';

export const createMessageKeyLeaseSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group_id BIGINT NOT NULL, -- PK
			message_key TEXT NOT NULL,         -- PK
			lease_token UUID NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (consumer_group_id, message_key)
		);
	`;

export function createMessageKeyLeaseSql(topicId: number): string {
	return interpolate(createMessageKeyLeaseSqlTemplate, messageKeyLeaseTable(topicId));
}
