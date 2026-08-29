// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { deliveryLogTable } from '../table-names';

export const createDeliveryLogAttemptIndexSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %s_attempt ON %s (consumer_group_id, message_id, attempt);
	`;

export function createDeliveryLogAttemptIndexSql(topicId: number): string {
	return interpolate(
		createDeliveryLogAttemptIndexSqlTemplate,
		deliveryLogTable(topicId),
		deliveryLogTable(topicId),
	);
}
