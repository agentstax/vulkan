import type {
	ClaimedMessage,
	ClaimedRange,
	DatabaseStage,
	RunResult,
	VulkanDatabase,
} from './database';

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

	connect(): Promise<VulkanDatabase> {
		this.connecting ??= this.create();
		return this.connecting;
	}

	// PGlite serializes statements on its own mutex, so two panels running at
	// once need no queue here
	async run(sql: string): Promise<RunResult> {
		const database = await this.connect();
		return database.run(sql);
	}

	async produce(description: string): Promise<void> {
		const database = await this.connect();
		await database.produce(description);
		this.revision += 1;
	}

	// a group is a write: its cursor row is what the cursor panel reads
	async registerGroup(name: string): Promise<void> {
		const database = await this.connect();
		await database.registerGroup(name);
		this.revision += 1;
	}

	// One tick: claim a range, call the handler once per message inside it, free
	// the lease. The three statements are the library's; this loop, and the
	// handler it hands each message to, are the page's.
	async tick(
		group: string,
		handle: (message: ClaimedMessage) => void,
	): Promise<ClaimedRange | null> {
		const database = await this.connect();
		const claimed = await database.claim(group);

		// caught up: the tick either wrote nothing or moved only the cursor's
		// proof columns, which neither panel reads
		if (claimed === null) return null;

		for (const message of claimed.messages) {
			handle(message);
		}

		await database.commit(claimed.groupId, claimed.token);
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
		const current = this.connecting;
		this.connecting = null;

		// a database that never came up has nothing to close, and its failure is
		// already the thing being closed away from
		await current?.then(
			(database) => database.close(),
			() => {},
		);
	}

	async listGroups(): Promise<string[]> {
		const database = await this.connect();
		return database.listGroups();
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
