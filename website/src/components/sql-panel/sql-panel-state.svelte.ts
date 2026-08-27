import { untrack } from 'svelte';
import { caughtMessage } from '../../helpers/caught-message';
import type { RunResult } from '../sandbox/database';
import type { DatabaseState } from '../sandbox/database-state.svelte';
import type { PanelShell } from '../sandbox/types';

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

	// the visitor changed the query the panel shipped with. Auto re-runs stop
	// here: running their SQL on every produce and tick is the panel deciding
	// when their query executes.
	edited = $state(false);

	// a write landed that this panel did not run, so its result is behind the
	// database. Only reachable while edited.
	stale = $state(false);

	private database: DatabaseState;

	private defaultSql: string;

	private lastRevision = -1;

	constructor(database: DatabaseState, shell: PanelShell) {
		this.database = database;
		this.table = shell.table;
		this.sql = shell.sql;
		this.defaultSql = shell.sql;
		this.result = {
			columns: shell.columns,
			rows: shell.rows,
			affectedRows: null,
			durationMs: null,
			statementCount: 1,
		};
	}

	// The caller passes the revision so that ITS effect is what subscribes to
	// the write count. Running is untracked on purpose: run() reads this.sql,
	// and a tracked read would re-run the query on every keystroke in the
	// editor.
	runAt(revision: number): void {
		if (revision === this.lastRevision) return;

		this.lastRevision = revision;
		if (this.edited) {
			this.stale = true;
			return;
		}

		untrack(() => void this.run());
	}

	// every keystroke arrives here from the editor; a document typed back to the
	// query the panel shipped with is not an edit, so auto re-runs resume
	setSql(next: string): void {
		this.sql = next;
		this.edited = next !== this.defaultSql;
		if (!this.edited) this.stale = false;
	}

	// a boot failure arrives here too, so the panel that asked is the one that
	// reports it
	async run(): Promise<void> {
		this.running = true;

		try {
			this.result = await this.database.run(this.sql);
			this.errorMessage = null;
			this.stale = false;
		} catch (caught) {
			this.errorMessage = caughtMessage(caught);
		} finally {
			this.running = false;
		}
	}
}
