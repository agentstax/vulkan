import { SvelteSet } from 'svelte/reactivity';
import type { DatabaseStage, VulkanDatabase } from './database';
import type { ClaimedMessage, ClaimedRange, RunResult } from './model';

// idle: nothing has asked for the database yet
// connecting: the wasm chunk is loading, or Postgres is starting
// ready: statements can run
// failed: the boot threw
export type DatabaseStatus = 'idle' | 'connecting' | 'ready' | 'failed';

// The sandbox's one database. Every panel runs its statements through this
// instance, so what one panel writes the next panel reads.
export class DatabaseState {
	status: DatabaseStatus = $state('idle');
	stage: DatabaseStage | null = $state(null);

	// bumped once per statement that WRITES, never by a panel's own read. A
	// panel watches it to learn its last result is stale.
	revision = $state(0);

	// the panels mount together and each asks to run: the first call owns the
	// boot and the rest await the same promise
	private connecting: Promise<VulkanDatabase> | null = null;

	// operations still running; close() waits them out, because closing
	// PGlite with a statement in flight leaves its wasm spinning on the
	// main thread
	private pendingOperations = new SvelteSet<Promise<unknown>>();

	// set while close() drains; a panel's straggler run scheduled during the
	// island's teardown is refused instead of reaching a closed database
	private closing = false;

	connect(): Promise<VulkanDatabase> {
		this.connecting ??= this.create();
		return this.connecting;
	}

	// the one path every statement-running verb takes: the operation is
	// registered before its first await, so close() sees it the moment it
	// exists and can wait for it
	private perform<T>(work: (database: VulkanDatabase) => Promise<T>): Promise<T> {
		if (this.closing) {
			return Promise.reject(new Error('the database is closing'));
		}

		const operation = this.connect().then((database) => work(database));
		this.pendingOperations.add(operation);
		void operation
			.catch(() => {})
			.finally(() => {
				this.pendingOperations.delete(operation);
			});
		return operation;
	}

	// PGlite serializes statements on its own mutex, so two panels running at
	// once need no queue here
	async run(sql: string): Promise<RunResult> {
		return this.perform((database) => database.run(sql));
	}

	async produce(description: string): Promise<void> {
		await this.perform((database) => database.produce(description));
		this.revision += 1;
	}

	// a group is a write: its cursor row is what the cursor panel reads
	async registerGroup(name: string): Promise<void> {
		await this.perform((database) => database.registerGroup(name));
		this.revision += 1;
	}

	// One tick: claim a range, call the handler once per message inside it, free
	// the lease. The three statements are the library's; this loop, and the
	// handler it hands each message to, are the page's.
	async tick(
		group: string,
		handle: (message: ClaimedMessage) => void,
	): Promise<ClaimedRange | null> {
		const claimed = await this.perform(async (database) => {
			const claimed = await database.claim(group);

			// caught up: the tick either wrote nothing or moved only the cursor's
			// proof columns, which neither panel reads
			if (claimed === null) return null;

			for (const message of claimed.messages) {
				handle(message);
			}

			await database.commit(claimed.groupId, claimed.token);
			return claimed;
		});
		if (claimed === null) return null;

		this.revision += 1;
		return claimed;
	}

	// Reset sandbox: the current Postgres is closed and a fresh one is built from
	// the seed. The wasm chunk is already in memory, so only the boot repeats --
	// and the bump is what sends both panels back to the database.
	async reset(): Promise<void> {
		this.status = 'connecting';
		await this.close();
		await this.connect();
		this.revision += 1;
	}

	// Drops the handle and releases the Postgres behind it, whose wasm memory is
	// 128 MB. The status is the caller's to set: reset is on its way back to
	// connecting, and a destroyed island has nothing left to render one.
	async close(): Promise<void> {
		// refuse new operations, then wait for every running one to settle
		// before shutting down -- re-checking because a settling operation
		// can have started another before the refusal began
		this.closing = true;
		while (this.pendingOperations.size > 0) {
			await Promise.allSettled([...this.pendingOperations]);
		}

		const current = this.connecting;
		this.connecting = null;

		// a database that never came up has nothing to close, and its failure is
		// already the thing being closed away from
		await current?.then(
			(database) => database.close(),
			() => {},
		);

		// reset builds a fresh database next; its operations are welcome again
		this.closing = false;
	}

	async listGroups(): Promise<string[]> {
		return this.perform((database) => database.listGroups());
	}

	private async create(): Promise<VulkanDatabase> {
		this.status = 'connecting';
		this.stage = 'downloading';

		try {
			const { createVulkanDatabase } = await import('./database');
			const database = await createVulkanDatabase((stage) => {
				this.stage = stage;
			});
			this.status = 'ready';
			this.stage = null;
			return database;
		} catch (caught) {
			// the rejected promise is kept, so every later caller reports this
			// same failure instead of each one restarting the boot
			this.status = 'failed';
			throw caught;
		}
	}
}
