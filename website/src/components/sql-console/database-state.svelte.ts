import type { DatabaseStage, RunResult, VulkanDatabase } from './database';

// idle: nothing has asked for the database yet
// connecting: the wasm chunk is loading, or Postgres is starting
// ready: statements can run
// failed: the boot threw
export type DatabaseStatus = 'idle' | 'connecting' | 'ready' | 'failed';

// The console's one database. Every panel runs its statements through this
// instance, so what one panel writes the next panel reads.
export class DatabaseState {
	status: DatabaseStatus = $state('idle');
	stage: DatabaseStage | null = $state(null);

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

	async produce(text: string): Promise<void> {
		const database = await this.connect();
		await database.produce(text);
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
