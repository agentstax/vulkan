import type { RunResult } from '../sql-console/database';
import type { DatabaseState } from '../sql-console/database-state.svelte';
import type { PanelShell } from '../sql-console/types';

// One panel's query and that query's last outcome. The shell seeds it with the
// build-time rows, so the panel reads correctly before its first run lands.
export class PanelState {
	readonly table: string;
	sql = $state('');
	result: RunResult = $state({
		columns: [],
		rows: [],
		affectedRows: null,
		durationMs: null,
		statementCount: 1,
	});
	errorMessage: string | null = $state(null);
	running = $state(false);

	private database: DatabaseState;

	constructor(database: DatabaseState, shell: PanelShell) {
		this.database = database;
		this.table = shell.table;
		this.sql = shell.sql;
		this.result = {
			columns: shell.columns,
			rows: shell.rows,
			affectedRows: null,
			durationMs: null,
			statementCount: 1,
		};
	}

	// a boot failure arrives here too, so the panel that asked is the one that
	// reports it
	async run(): Promise<void> {
		this.running = true;

		try {
			this.result = await this.database.run(this.sql);
			this.errorMessage = null;
		} catch (caught) {
			this.errorMessage = caught instanceof Error ? caught.message : String(caught);
		} finally {
			this.running = false;
		}
	}
}
