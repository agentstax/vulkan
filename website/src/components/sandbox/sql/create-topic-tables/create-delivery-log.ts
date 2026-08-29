// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { deliveryLogTable } from '../table-names';

export const createDeliveryLogSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL,
			message_id BIGINT NOT NULL,
			attempt INT NOT NULL,                 -- the run this event belongs to; a claim handed back at the key gate logs under the number it returned
			status TEXT NOT NULL DEFAULT 'failure',
			error TEXT NOT NULL,
			attempted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`;

export function createDeliveryLogSql(topicId: number): string {
	return interpolate(createDeliveryLogSqlTemplate, deliveryLogTable(topicId));
}
