import type { ResultRow } from './database';

// one panel's server-rendered starting point: the table it reads, the query
// that reads it, and that query's real output at build time
export type PanelShell = {
	table: string;
	sql: string;
	columns: string[];
	rows: ResultRow[];
};
