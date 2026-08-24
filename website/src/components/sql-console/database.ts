// The console's database: a real Postgres (PGlite, wasm) created from the
// library's own DDL and seeded through its own produce statement. The same
// module runs in Node at build time -- the static shell's rows are this
// database's real output -- and in the browser on the console's first Run.
import type { PGlite } from '@electric-sql/pglite';
import { createSystemTablesStatements } from './sql/create-system-tables/statements';
import { createTopicTablesStatements } from './sql/create-topic-tables/statements';
import { protectedInsertKeylessSql } from './sql/protected-insert-keyless';
import { protectedInsertKeyedSql } from './sql/protected-insert-keyed';

// the seeded demo topic: id 1, the library's default partition size
const demoTopicId = 1;
const demoPartitionSize = 1_000_000;

export const demoTopicName = 'orders';

export const messageLogSql = `SELECT id, routing_key, payload
FROM message_log_1
ORDER BY id DESC;`;

export const cursorSql = `SELECT g.name, c.claimed
FROM cursor_1 c
JOIN consumer_group g ON g.id = c.consumer_group_id;`;

export type DatabaseStage = 'downloading' | 'starting postgres' | 'creating tables';

type GroupRow = { id: number; topic_id: number; name: string; created_at: Date };
type GroupNameRow = { name: string };

export type RunResult = {
	columns: string[];
	rows: (string | null)[][];
	affectedRows: number | null;
	durationMs: number | null;
	statementCount: number;
};

export class VulkanDatabase {
	private db: PGlite;

	constructor(db: PGlite) {
		this.db = db;
	}

	async run(sql: string): Promise<RunResult> {
		const start = performance.now();
		const results = await this.db.exec(sql);
		const durationMs = performance.now() - start;

		const last = results.at(-1);
		if (last === undefined) {
			return { columns: [], rows: [], affectedRows: 0, durationMs, statementCount: 0 };
		}

		const columns = last.fields.map((field) => field.name);
		const resultRows = last.rows as Record<string, unknown>[];
		const rows = resultRows.map((row) => last.fields.map((field) => toCell(row[field.name])));
		return {
			columns,
			rows,
			affectedRows: columns.length === 0 ? (last.affectedRows ?? 0) : null,
			durationMs,
			statementCount: results.length,
		};
	}

	// the reader's own message, produced through the same statement the seed
	// uses -- a fresh idempotency key every time, so every click lands a row
	async produce(text: string): Promise<void> {
		await this.db.query(protectedInsertKeylessSql(demoTopicId), [
			crypto.randomUUID(),
			JSON.stringify({ text }),
			'',
			null,
		]);
	}

	// registerGroup's own shape: the advisory lock, the re-check under it, the
	// group insert and the cursor row, all in one transaction. The cursor row
	// is why a new group replays -- it starts at claimed 0.
	async registerGroup(name: string): Promise<void> {
		await this.db.transaction(async (tx) => {
			await tx.query(
				`SELECT pg_advisory_xact_lock(hashtext(format('consumer_group:%s:%s', $1::bigint, $2::text)));`,
				[demoTopicId, name],
			);

			const found = await tx.query<GroupRow>(
				`SELECT id, topic_id, name, created_at FROM consumer_group WHERE topic_id = $1 AND name = $2;`,
				[demoTopicId, name],
			);
			if (found.rows.length > 0) return;

			const inserted = await tx.query<GroupRow>(
				`INSERT INTO consumer_group (topic_id, name) VALUES ($1, $2) RETURNING id, topic_id, name, created_at;`,
				[demoTopicId, name],
			);
			await tx.query(`INSERT INTO cursor_${demoTopicId} (consumer_group_id) VALUES ($1);`, [
				inserted.rows[0]!.id,
			]);
		});
	}

	async listGroups(): Promise<string[]> {
		const groups = await this.db.query<GroupNameRow>(
			`SELECT name FROM consumer_group WHERE topic_id = $1 ORDER BY id;`,
			[demoTopicId],
		);
		return groups.rows.map((group) => group.name);
	}

	async close(): Promise<void> {
		await this.db.close();
	}
}

export async function createVulkanDatabase(
	onStage: (stage: DatabaseStage) => void,
): Promise<VulkanDatabase> {
	onStage('downloading');
	const { PGlite } = await import('@electric-sql/pglite');

	onStage('starting postgres');
	const db = await PGlite.create();

	onStage('creating tables');
	for (const statement of createSystemTablesStatements) {
		await db.exec(statement);
	}
	for (const statement of createTopicTablesStatements(demoTopicId, demoPartitionSize)) {
		await db.exec(statement);
	}
	const database = new VulkanDatabase(db);
	await seed(db);

	// the demo group goes through the same verb the reader's Add uses, so it
	// gets the cursor row a plain catalog INSERT leaves out -- and registering
	// it after the messages is the replay case: its cursor starts at 0
	await database.registerGroup('billing');
	return database;
}

// ***************
// *** HELPERS ***
// ***************

// catalog rows are plain setup inserts; the messages go through the library's
// own produce statement -- that path is the page's claim, so it stays verbatim
async function seed(db: PGlite): Promise<void> {
	await db.query(`INSERT INTO system DEFAULT VALUES`);
	await db.query(
		`INSERT INTO topic (system_id, name, schema_version, partition_size) VALUES ($1, $2, $3, $4)`,
		[1, demoTopicName, 1, demoPartitionSize],
	);

	type ProducedRow = { id: number };
	await db.query<ProducedRow>(protectedInsertKeylessSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 42, "amount_cents": 1999}',
		'orders.eu.created',
		null,
	]);
	await db.query<ProducedRow>(protectedInsertKeylessSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 43, "amount_cents": 250}',
		'',
		null,
	]);
	await db.query<ProducedRow>(protectedInsertKeyedSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 42, "status": "paid"}',
		'orders.eu.updated',
		'order-42',
		0,
		null,
	]);
	await db.query<ProducedRow>(protectedInsertKeyedSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 42, "status": "paid"}',
		'orders.eu.updated',
		'order-42',
		0,
		null,
	]);
	await db.query<ProducedRow>(protectedInsertKeyedSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 42, "status": "paid"}',
		'orders.eu.updated',
		'order-42',
		0,
		null,
	]);
	await db.query<ProducedRow>(protectedInsertKeyedSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 42, "status": "paid"}',
		'orders.eu.updated',
		'order-42',
		0,
		null,
	]);
	await db.query<ProducedRow>(protectedInsertKeyedSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 42, "status": "paid"}',
		'orders.eu.updated',
		'order-42',
		0,
		null,
	]);
	await db.query<ProducedRow>(protectedInsertKeyedSql(demoTopicId), [
		crypto.randomUUID(),
		'{"order_id": 42, "status": "paid"}',
		'orders.eu.updated',
		'order-42',
		0,
		null,
	]);
}

function toCell(value: unknown): string | null {
	if (value === null || value === undefined) return null;
	if (value instanceof Date) return value.toISOString();
	if (typeof value === 'object') return JSON.stringify(value);
	return String(value);
}
