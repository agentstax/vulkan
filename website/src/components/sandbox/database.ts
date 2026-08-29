// The sandbox's database: a real Postgres (PGlite, wasm) created from the
// library's own DDL and seeded through its own produce statement. The same
// module runs in Node at build time -- the static shell's rows are this
// database's real output -- and in the browser when the sandbox mounts.
import type { PGlite } from '@electric-sql/pglite';
import type {
	ClaimedRange,
	CursorRow,
	GroupNameRow,
	GroupRow,
	LeaseRow,
	MessageRow,
	ProducedRow,
	ResultCell,
	RunResult,
	SnapshotRow,
} from './model';
import { claimCursorSql } from './sql/claim-cursor';
import { claimLeaseSql } from './sql/claim-lease';
import { claimSnapshotSql } from './sql/claim-snapshot';
import { createSystemTablesStatements } from './sql/create-system-tables/statements';
import { createTopicTablesStatements } from './sql/create-topic-tables/statements';
import { freeLeaseSql } from './sql/free-lease';
import { getGroupSql } from './sql/get-group';
import { protectedInsertUncompactedSql } from './sql/protected-insert-uncompacted';
import { readMessagesSql } from './sql/read-messages';
import { insertCursorBeginningSql } from './sql/register-group-cursor';
import { registerGroupInsertSql } from './sql/register-group-insert';
import { registerGroupLockSql } from './sql/register-group-lock';

// the seeded demo topic: id 1, the library's default partition size
const demoTopicId = 1;
const demoPartitionSize = 1_000_000;

// ConsumerConfig.BatchLimit's default -- claimed advances by one id per claim,
// so one tick is one step
const batchLimit = 1;

// what consumer_runner claims with under the library's defaults: MessageMax
// Timeout 30s + TimeoutGrace 100ms + QueueMargin 5s + RecordMargin 2s
const leaseSeconds = 37.1;

// the seeded log, one order per message, all through the library's own
// uncompacted produce statement -- no seed sets a message key.
const seedOrders = [
	'ship pallet',
	'refund card',
	'pack crate',
	'restock shelf',
	'void invoice',
	'split shipment',
	'hold for pickup',
	'reprint label',
];

// order numbers are the payload's own counter, not a second spelling of the
// message id -- four digits so the two never read as the same number
const firstOrderId = 4001;

export const demoTopicName = 'orders';

// routing_key is left out: nothing here declares a binding, so it selects
// nothing, and a column beside a claim reads as though it did
export const messageLogSql = `SELECT id, payload
FROM message_log_1
ORDER BY id DESC;`;

export const cursorSql = `SELECT g.name, c.claimed
FROM consumer_group_cursor_1 c
JOIN consumer_group_config g ON g.id = c.consumer_group_id;`;

export type DatabaseStage = 'downloading' | 'starting postgres' | 'creating tables';

// what the pool and a transaction can both do -- the sandbox's sibling of the
// library's datastore.Querier seam
type Querier = Pick<PGlite, 'query'>;

export class VulkanDatabase {
	private db: PGlite;

	// order numbers continue where the seed left off, so a produced message is
	// the next order rather than one that restarts the count
	private nextOrderId = firstOrderId + seedOrders.length;

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
	async produce(description: string): Promise<void> {
		await this.db.query(protectedInsertUncompactedSql(demoTopicId), [
			crypto.randomUUID(),
			orderPayload(this.nextOrderId, description),
			'orders.eu.created',
			'',
			null,
		]);
		this.nextOrderId += 1;
	}

	// registerGroup's own shape, statement for statement: the look-up, the
	// advisory lock, the re-check under it, the group insert and the cursor row,
	// all in one transaction. The cursor row is why a new group replays -- it
	// starts at claimed 0. One backend never contends the lock, so the re-check
	// is here for fidelity, not for a race this page can have.
	async registerGroup(name: string): Promise<void> {
		await this.db.transaction(async (tx) => {
			if ((await this.getGroup(tx, name)) !== null) return;

			await tx.query(registerGroupLockSql, [demoTopicId, name]);
			if ((await this.getGroup(tx, name)) !== null) return;

			const inserted = await tx.query<GroupRow>(registerGroupInsertSql, [demoTopicId, name]);
			await tx.query(insertCursorBeginningSql(demoTopicId), [inserted.rows[0]!.id]);
		});
	}

	// ClaimMessagesWithCursor's fresh-claim path: the snapshot pair, the gate
	// that proves it, the lease over the range it opens, and the rows inside it.
	// The reclaim path the library tries first is skipped -- commit frees the
	// lease in the same tick that took it, so none is ever left to expire.
	async claim(groupName: string): Promise<ClaimedRange | null> {
		const group = await this.getGroup(this.db, groupName);
		if (group === null) {
			throw new Error(
				`consumer group not found: ${JSON.stringify(groupName)} — register it with the Add consumer control`,
			);
		}

		return this.db.transaction(async (tx) => {
			// a pure SELECT, so this transaction still holds no txid when it reads
			// xmax -- the whole gate below depends on that
			const snapshot = await tx.query<SnapshotRow>(claimSnapshotSql(demoTopicId), [group.id]);
			const pair = snapshot.rows[0];
			if (pair === undefined) throw noCursor(group.id);

			// this snapshot saw the head already proven and fully claimed: the gate
			// never runs, so the tick writes nothing at all
			if (
				pair.head === pair.pending_head &&
				pair.pending_head === pair.settled_head &&
				pair.claimed === pair.settled_head
			) {
				return null;
			}

			const advanced = await tx.query<CursorRow>(claimCursorSql(demoTopicId), [
				group.id,
				batchLimit,
				pair.head,
				pair.xmax,
			]);
			const range = advanced.rows[0];
			if (range === undefined) throw noCursor(group.id);

			// low === high is caught up: the cursor's proof columns moved, but
			// claimed did not, so there is no range to lease
			if (range.low === range.high) return null;

			const lease = await tx.query<LeaseRow>(claimLeaseSql(demoTopicId), [
				group.id,
				range.low,
				range.high,
				leaseSeconds,
			]);
			const messages = await tx.query<MessageRow>(readMessagesSql(demoTopicId), [
				range.low,
				range.high,
				group.id,
			]);

			return {
				groupId: group.id,
				token: lease.rows[0]!.token,
				low: range.low,
				high: range.high,
				messages: messages.rows.map((row) => ({ id: row.id, payload: row.payload })),
			};
		});
	}

	// Commit frees the range's lease and nothing else. The handler above succeeds
	// on every message, and under the demo topic's delivery_log_mode -- the
	// library default 'failures' -- a successful outcome is never collected, so
	// commit's batch is empty: no exception_queue_1 row and no delivery_log_1 row.
	async commit(groupId: number, token: string): Promise<void> {
		const freed = await this.db.query(freeLeaseSql(demoTopicId), [groupId, token]);
		if (freed.affectedRows === 0) {
			throw new Error('lease lost to another consumer');
		}
	}

	async listGroups(): Promise<string[]> {
		const groups = await this.db.query<GroupNameRow>(
			`SELECT name FROM consumer_group_config WHERE topic_id = $1 ORDER BY id;`,
			[demoTopicId],
		);
		return groups.rows.map((group) => group.name);
	}

	private async getGroup(q: Querier, name: string): Promise<GroupRow | null> {
		const found = await q.query<GroupRow>(getGroupSql, [demoTopicId, name]);
		return found.rows[0] ?? null;
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

// a group whose cursor row is missing -- the one state that makes a consumer
// poll forever while messages pile up
function noCursor(groupId: number): Error {
	return new Error(
		`cursor not found for group ${groupId} on topic ${demoTopicId} — register the group before claiming`,
	);
}

// catalog rows are plain setup inserts; the messages go through the library's
// own produce statement -- that path is the page's claim, so it stays verbatim
async function seed(db: PGlite): Promise<void> {
	await db.query(`INSERT INTO system_config DEFAULT VALUES`);
	await db.query(
		`INSERT INTO topic_config (system_id, name, schema_version, partition_size) VALUES ($1, $2, $3, $4)`,
		[1, demoTopicName, 1, demoPartitionSize],
	);

	for (const [index, description] of seedOrders.entries()) {
		const orderId = firstOrderId + index;
		await db.query<ProducedRow>(protectedInsertUncompactedSql(demoTopicId), [
			crypto.randomUUID(),
			orderPayload(orderId, description),
			'orders.eu.created',
			'',
			null,
		]);
	}
}

function orderPayload(orderId: number, description: string): string {
	return JSON.stringify({ order_id: orderId, desc: description });
}

function toCell(value: unknown): ResultCell {
	if (value === null || value === undefined) return null;
	if (value instanceof Date) return value.toISOString();
	if (typeof value === 'object') return JSON.stringify(value);
	return String(value);
}
