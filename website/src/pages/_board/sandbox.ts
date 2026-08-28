// The static shell's content is a real run: the same database module the
// browser sandbox uses is created here in Node at every build, so the shell
// rows are actual query output and a broken statement fails the build.
import {
	createVulkanDatabase,
	cursorSql,
	demoTopicName,
	messageLogSql,
} from '../../components/sandbox/database';
import type { VulkanDatabase } from '../../components/sandbox/database';
import type { PanelShell } from '../../components/sandbox/types';

export const sandboxTopic = demoTopicName;

export type SandboxShell = {
	messages: PanelShell;
	cursors: PanelShell;
};

export async function sandboxShell(): Promise<SandboxShell> {
	const database = await createVulkanDatabase(() => {});
	const messages = await panelShell(database, 'message_log_1', messageLogSql);
	const cursors = await panelShell(database, 'cursor_1', cursorSql);
	await database.close();
	return { messages, cursors };
}

// ***************
// *** HELPERS ***
// ***************

async function panelShell(
	database: VulkanDatabase,
	table: string,
	sql: string,
): Promise<PanelShell> {
	const result = await database.run(sql);
	return { table, sql, columns: result.columns, rows: result.rows };
}
