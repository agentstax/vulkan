import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, test } from 'vitest';
import { createSystemTablesStatements } from './create-system-tables/statements';
import { createTableSqlTemplate } from './create-topic-tables/create-table';
import { createPartitionSqlTemplate } from './create-topic-tables/create-partition';
import { createCompactionKeyIndexSqlTemplate } from './create-topic-tables/create-compaction-key-index';
import { createIdempotencyKeySqlTemplate } from './create-topic-tables/create-idempotency-key';
import { createIdempotencyKeyCreatedAtIndexSqlTemplate } from './create-topic-tables/create-idempotency-key-created-at-index';
import { createDeliverySqlTemplate } from './create-topic-tables/create-delivery';
import { createDeliveryLogSqlTemplate } from './create-topic-tables/create-delivery-log';
import { createCursorSqlTemplate } from './create-topic-tables/create-cursor';
import { createLeaseSqlTemplate } from './create-topic-tables/create-lease';
import { createKeyLeaseSqlTemplate } from './create-topic-tables/create-key-lease';
import { createCompactionHeadSqlTemplate } from './create-topic-tables/create-compaction-head';
import { createBindingSqlTemplate } from './create-topic-tables/create-binding';
import { createBindingLogSqlTemplate } from './create-topic-tables/create-binding-log';
import { createBindingLogIndexSqlTemplate } from './create-topic-tables/create-binding-log-index';
import { protectedInsertKeyedSqlTemplate } from './protected-insert-keyed';
import { protectedInsertKeylessSqlTemplate } from './protected-insert-keyless';
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
	createCompactionKeyIndexSqlTemplate,
	createIdempotencyKeySqlTemplate,
	createIdempotencyKeyCreatedAtIndexSqlTemplate,
	createDeliverySqlTemplate,
	createDeliveryLogSqlTemplate,
	createCursorSqlTemplate,
	createLeaseSqlTemplate,
	createKeyLeaseSqlTemplate,
	createCompactionHeadSqlTemplate,
	createBindingSqlTemplate,
	createBindingLogSqlTemplate,
	createBindingLogIndexSqlTemplate,
];

const protectedInsertTemplates = [
	protectedInsertKeyedSqlTemplate,
	protectedInsertKeylessSqlTemplate,
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
// the console mirrors named verbs, not whole files, so group.go's deleteGroup and
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
