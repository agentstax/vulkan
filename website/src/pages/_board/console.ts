// The static shell's content is a real run: the same database module the
// browser console uses is created here in Node at every build, so the shell
// rows are actual query output and a broken statement fails the build.
import { createVulkanDatabase, exampleSql } from '../../components/sql-console/database';

export const consoleLabel = 'your queue, selected';

export type ConsoleShell = {
	sql: string;
	columns: string[];
	rows: (string | null)[][];
};

export async function consoleShell(): Promise<ConsoleShell> {
	const database = await createVulkanDatabase(() => {});
	const result = await database.run(exampleSql);
	await database.close();
	return { sql: exampleSql, columns: result.columns, rows: result.rows };
}
