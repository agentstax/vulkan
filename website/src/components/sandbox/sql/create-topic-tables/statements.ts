// the statement order of createTopicTables -- one entry per Exec in the Go method
import { createTableSql } from './create-table';
import { createPartitionSql } from './create-partition';
import { createMessageKeyIndexSql } from './create-message-key-index';
import { createIdempotencyKeySql } from './create-idempotency-key';
import { createIdempotencyKeyCreatedAtIndexSql } from './create-idempotency-key-created-at-index';
import { createExceptionQueueSql } from './create-exception-queue';
import { createDeliveryLogSql } from './create-delivery-log';
import { createConsumerGroupCursorSql } from './create-consumer-group-cursor';
import { createClaimLeaseSql } from './create-claim-lease';
import { createMessageKeyLeaseSql } from './create-message-key-lease';
import { createCompactionHeadSql } from './create-compaction-head';
import { createBindingConfigSql } from './create-binding-config';
import { createBindingConfigLogSql } from './create-binding-config-log';
import { createBindingConfigLogIndexSql } from './create-binding-config-log-index';

export function createTopicTablesStatements(topicId: number, partitionSize: number): string[] {
	return [
		createTableSql(topicId),
		createPartitionSql(topicId, partitionSize),
		createMessageKeyIndexSql(topicId),
		createIdempotencyKeySql(topicId),
		createIdempotencyKeyCreatedAtIndexSql(topicId),
		createExceptionQueueSql(topicId),
		createDeliveryLogSql(topicId),
		createConsumerGroupCursorSql(topicId),
		createClaimLeaseSql(topicId),
		createMessageKeyLeaseSql(topicId),
		createCompactionHeadSql(topicId),
		createBindingConfigSql(topicId),
		createBindingConfigLogSql(topicId),
		createBindingConfigLogIndexSql(topicId),
	];
}
