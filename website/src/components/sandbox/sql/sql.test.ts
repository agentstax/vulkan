import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, test } from 'vitest';
import {
	createSystemTablesStatements,
	createSystemTablesTemplates,
} from './create-system-tables/statements';
import {
	createTopicTablesStatements,
	createTopicTablesTemplates,
} from './create-topic-tables/statements';
import { protectedInsertCompactedSqlTemplate } from './protected-insert-compacted';
import { protectedInsertUncompactedSqlTemplate } from './protected-insert-uncompacted';
import { getGroupSqlTemplate } from './get-group';
import { registerGroupLockSql } from './register-group-lock';
import { registerGroupInsertSqlTemplate } from './register-group-insert';
import {
	insertCursorBeginningSqlTemplate,
	insertCursorHeadSqlTemplate,
} from './register-group-cursor';
import { claimSnapshotSqlTemplate } from './claim-snapshot';
import { claimCursorSqlTemplate } from './claim-cursor';
import { claimLeaseSqlTemplate } from './claim-lease';
import { readMessagesSqlTemplate } from './read-messages';
import { freeLeaseSqlTemplate } from './free-lease';
import { interpolate } from './interpolate';

const protectedInsertTemplates = [
	protectedInsertCompactedSqlTemplate,
	protectedInsertUncompactedSqlTemplate,
];

const registerGroupTemplates = [registerGroupLockSql, registerGroupInsertSqlTemplate];

const insertCursorTemplates = [insertCursorBeginningSqlTemplate, insertCursorHeadSqlTemplate];

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
			createSystemTablesTemplates,
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
		[
			'consumergroup.getGroup',
			'pkg/consumergroup/controller/datastore/group.go',
			[getGroupSqlTemplate],
		],
		[
			'consumergroup.registerGroup',
			'pkg/consumergroup/controller/datastore/group.go',
			registerGroupTemplates,
		],
		[
			'consumergroup.insertCursor',
			'pkg/consumergroup/controller/datastore/group.go',
			insertCursorTemplates,
		],
		[
			'messageconsumer.freshClaimMessagesWithCursor',
			'pkg/consumergroup/messageconsumer/controller/datastore/fresh_claim.go',
			[claimSnapshotSqlTemplate, claimCursorSqlTemplate],
		],
		[
			'messageconsumer.claimMessages',
			'pkg/consumergroup/messageconsumer/controller/datastore/fresh_claim.go',
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

describe('interpolate fills a template the way fmt.Sprintf does', () => {
	test('verb [1] is the schema, and the caller never passes it', () => {
		expect(interpolate('FROM %[1]s.topic_config')).toBe('FROM public.topic_config');
	});

	test('values fill [2] onward, in order, and repeat where the verb repeats', () => {
		expect(interpolate('%[1]s.%[2]s JOIN %[1]s.%[3]s ON %[2]s.id', 'a', 'b')).toBe(
			'public.a JOIN public.b ON a.id',
		);
	});

	test('%d carries a number', () => {
		expect(interpolate('TO (%[2]d)', 512)).toBe('TO (512)');
	});

	// a template whose verbs outrun its values is drift, not a rendering choice
	test('a missing value throws rather than reaching PGlite half-filled', () => {
		expect(() => interpolate('%[1]s.%[2]s AND %[3]s', 'only-one')).toThrow(/only 1 values/);
	});
});

// each statements.ts holds two lists walking one Go method: the raw templates
// and the filled statements. Co-locating them makes a one-sided edit visible;
// this is what makes it fail.
describe('a statements.ts keeps its two lists in step', () => {
	test('createSystemTables', () => {
		expect(createSystemTablesStatements()).toHaveLength(createSystemTablesTemplates.length);
	});

	test('createTopicTables', () => {
		expect(createTopicTablesStatements(1, 1000)).toHaveLength(createTopicTablesTemplates.length);
	});
});

// a *SqlTemplate holds Sprintf verbs and exists for the drift test above;
// database.ts executes statements, so a template reaching it is SQL with a
// literal %[1]s in it -- a PGlite syntax error at prerender, which is how
// getGroupSql and registerGroupInsertSql broke once they gained a schema
test('database.ts executes filled statements, never raw templates', () => {
	const source = readFileSync(fileURLToPath(new URL('../database.ts', import.meta.url)), 'utf8');
	const templates = source.match(/\b\w+SqlTemplate\b/g) ?? [];
	expect(templates).toEqual([]);
});
