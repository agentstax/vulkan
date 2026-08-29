import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, test } from 'vitest';
import { createSystemTablesStatements } from './create-system-tables/statements';
import { createTableSqlTemplate } from './create-topic-tables/create-table';
import { createPartitionSqlTemplate } from './create-topic-tables/create-partition';
import { createMessageKeyIndexSqlTemplate } from './create-topic-tables/create-message-key-index';
import { createIdempotencyKeySqlTemplate } from './create-topic-tables/create-idempotency-key';
import { createIdempotencyKeyCreatedAtIndexSqlTemplate } from './create-topic-tables/create-idempotency-key-created-at-index';
import { createExceptionQueueSqlTemplate } from './create-topic-tables/create-exception-queue';
import { createDeliveryLogSqlTemplate } from './create-topic-tables/create-delivery-log';
import { createDeliveryLogAttemptIndexSqlTemplate } from './create-topic-tables/create-delivery-log-attempt-index';
import { createConsumerGroupCursorSqlTemplate } from './create-topic-tables/create-consumer-group-cursor';
import { createClaimLeaseSqlTemplate } from './create-topic-tables/create-claim-lease';
import { createMessageKeyLeaseSqlTemplate } from './create-topic-tables/create-message-key-lease';
import { createCompactionHeadSqlTemplate } from './create-topic-tables/create-compaction-head';
import { createBindingConfigSqlTemplate } from './create-topic-tables/create-binding-config';
import { createBindingConfigLogSqlTemplate } from './create-topic-tables/create-binding-config-log';
import { createBindingConfigLogIndexSqlTemplate } from './create-topic-tables/create-binding-config-log-index';
import { protectedInsertCompactedSqlTemplate } from './protected-insert-compacted';
import { protectedInsertUncompactedSqlTemplate } from './protected-insert-uncompacted';
import { getGroupSql } from './get-group';
import { registerGroupLockSql } from './register-group-lock';
import { registerGroupInsertSql } from './register-group-insert';
import { registerGroupCursorSqlTemplate } from './register-group-cursor';
import { claimSnapshotSqlTemplate } from './claim-snapshot';
import { claimCursorSqlTemplate } from './claim-cursor';
import { claimLeaseSqlTemplate } from './claim-lease';
import { readMessagesSqlTemplate } from './read-messages';
import { freeLeaseSqlTemplate } from './free-lease';

const createTopicTablesTemplates = [
	createTableSqlTemplate,
	createPartitionSqlTemplate,
	createMessageKeyIndexSqlTemplate,
	createIdempotencyKeySqlTemplate,
	createIdempotencyKeyCreatedAtIndexSqlTemplate,
	createExceptionQueueSqlTemplate,
	createDeliveryLogSqlTemplate,
	createDeliveryLogAttemptIndexSqlTemplate,
	createConsumerGroupCursorSqlTemplate,
	createClaimLeaseSqlTemplate,
	createMessageKeyLeaseSqlTemplate,
	createCompactionHeadSqlTemplate,
	createBindingConfigSqlTemplate,
	createBindingConfigLogSqlTemplate,
	createBindingConfigLogIndexSqlTemplate,
];

const protectedInsertTemplates = [
	protectedInsertCompactedSqlTemplate,
	protectedInsertUncompactedSqlTemplate,
];

const registerGroupTemplates = [
	registerGroupLockSql,
	registerGroupInsertSql,
	registerGroupCursorSqlTemplate,
];

function goSource(repoPath: string): string {
	return readFileSync(
		fileURLToPath(new URL(`../../../../../${repoPath}`, import.meta.url)),
		'utf8',
	);
}

// backticks in Go comments produce bogus segments; every real statement carries
// the -- vulkan: owner tag, and the owner is what the count is taken against:
// the site mirrors named verbs, not whole files, so group.go's deleteGroup and
// commit.go's partialCommit are absent here without weakening the count.
function goLiterals(source: string, owner: string): string[] {
	const parts = source.split('`');
	const literals: string[] = [];
	for (let index = 1; index < parts.length; index += 2) {
		const literal = parts[index];
		if (literal !== undefined && literal.includes(`-- vulkan: ${owner}`)) literals.push(literal);
	}
	return literals;
}

describe('embedded SQL matches the Go source byte-exact', () => {
	const cases: [string, string, string[]][] = [
		[
			'system.createSystemTables',
			'pkg/system/controller/datastore/tables.go',
			createSystemTablesStatements,
		],
		[
			'topic.createTopicTables',
			'pkg/topic/controller/datastore/tables.go',
			createTopicTablesTemplates,
		],
		[
			'producer.protectedInsert',
			'pkg/producer/controller/datastore/insert.go',
			protectedInsertTemplates,
		],
		['consumergroup.getGroup', 'pkg/consumergroup/controller/datastore/group.go', [getGroupSql]],
		[
			'consumergroup.registerGroup',
			'pkg/consumergroup/controller/datastore/group.go',
			registerGroupTemplates,
		],
		[
			'messageconsumer.freshClaimMessagesWithCursor',
			'pkg/consumergroup/messageconsumer/controller/datastore/freshclaim.go',
			[claimSnapshotSqlTemplate, claimCursorSqlTemplate],
		],
		[
			'messageconsumer.claimMessages',
			'pkg/consumergroup/messageconsumer/controller/datastore/freshclaim.go',
			[claimLeaseSqlTemplate],
		],
		[
			'messageconsumer.readMessages',
			'pkg/consumergroup/messageconsumer/controller/datastore/claim.go',
			[readMessagesSqlTemplate],
		],
		[
			'messageconsumer.commit',
			'pkg/consumergroup/messageconsumer/controller/datastore/commit.go',
			[freeLeaseSqlTemplate],
		],
	];

	test.each(cases)('%s', (owner, repoPath, embedded) => {
		const source = goSource(repoPath);
		for (const literal of embedded) {
			expect(source).toContain(literal);
		}
		// count both ways: a statement added to the verb on the Go side fails here too
		expect(goLiterals(source, owner)).toHaveLength(embedded.length);
	});
});
