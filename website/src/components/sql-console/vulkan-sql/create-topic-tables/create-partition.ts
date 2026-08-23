// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { messageLogPartitionTable, messageLogTable } from '../table-names';

export const createPartitionSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (0) TO (%d);
	`;

export function createPartitionSql(topicId: number, partitionSize: number): string {
	return interpolate(createPartitionSqlTemplate, messageLogPartitionTable(topicId, 0), messageLogTable(topicId), partitionSize);
}
