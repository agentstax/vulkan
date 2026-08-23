// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { deliveryLogTable } from '../table-names';

export const createDeliveryLogSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group_id BIGINT NOT NULL,    -- PK
			message_id BIGINT NOT NULL,           -- PK
			attempt INT NOT NULL,                 -- PK
			status TEXT NOT NULL DEFAULT 'failure',
			error TEXT NOT NULL,
			attempted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group_id, message_id, attempt)
		);
	`;

export function createDeliveryLogSql(topicId: number): string {
	return interpolate(createDeliveryLogSqlTemplate, deliveryLogTable(topicId));
}
