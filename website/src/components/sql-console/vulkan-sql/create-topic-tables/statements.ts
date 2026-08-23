// the statement order of createTopicTables -- one entry per Exec in the Go method
import { createTableSql } from './create-table';
import { createPartitionSql } from './create-partition';
import { createCompactionKeyIndexSql } from './create-compaction-key-index';
import { createIdempotencyKeySql } from './create-idempotency-key';
import { createIdempotencyKeyCreatedAtIndexSql } from './create-idempotency-key-created-at-index';
import { createDeliverySql } from './create-delivery';
import { createDeliveryLogSql } from './create-delivery-log';
import { createCursorSql } from './create-cursor';
import { createLeaseSql } from './create-lease';
import { createKeyLeaseSql } from './create-key-lease';
import { createCompactionHeadSql } from './create-compaction-head';
import { createBindingSql } from './create-binding';
import { createBindingLogSql } from './create-binding-log';
import { createBindingLogIndexSql } from './create-binding-log-index';

export function createTopicTablesStatements(topicId: number, partitionSize: number): string[] {
	return [
		createTableSql(topicId),
		createPartitionSql(topicId, partitionSize),
		createCompactionKeyIndexSql(topicId),
		createIdempotencyKeySql(topicId),
		createIdempotencyKeyCreatedAtIndexSql(topicId),
		createDeliverySql(topicId),
		createDeliveryLogSql(topicId),
		createCursorSql(topicId),
		createLeaseSql(topicId),
		createKeyLeaseSql(topicId),
		createCompactionHeadSql(topicId),
		createBindingSql(topicId),
		createBindingLogSql(topicId),
		createBindingLogIndexSql(topicId),
	];
}
