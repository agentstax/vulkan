import type { DatabaseStage, RunResult, VulkanDatabase } from './database';

// shell: server-rendered, editor not loaded yet -- Run stays disabled
// ready: the editor took over the static SQL text
// connecting: first Run is creating the browser database
// running: a statement is executing
// ran / error: the last Run's outcome
export type ConsolePhase = 'shell' | 'ready' | 'connecting' | 'running' | 'ran' | 'error';

export class ConsoleState {
	sql = $state('');
	phase: ConsolePhase = $state('shell');
	stage: DatabaseStage | null = $state(null);
	errorMessage: string | null = $state(null);
	result: RunResult = $state({
		columns: [],
		rows: [],
		affectedRows: null,
		durationMs: null,
		statementCount: 1,
	});
	private database: VulkanDatabase | null = null;

	constructor(sql: string, columns: string[], rows: (string | null)[][]) {
		this.sql = sql;
		this.result = { columns, rows, affectedRows: null, durationMs: null, statementCount: 1 };
	}

	editorReady(): void {
		if (this.phase === 'shell') this.phase = 'ready';
	}

	async run(): Promise<void> {
		try {
			if (this.database === null) {
				this.stage = 'downloading';
				this.phase = 'connecting';
				const { createVulkanDatabase } = await import('./database');
				this.database = await createVulkanDatabase((stage) => {
					this.stage = stage;
				});
			}

			this.phase = 'running';
			this.result = await this.database.run(this.sql);
			this.errorMessage = null;
			this.phase = 'ran';
		} catch (caught) {
			this.errorMessage = caught instanceof Error ? caught.message : String(caught);
			this.phase = 'error';
		}
	}
}
