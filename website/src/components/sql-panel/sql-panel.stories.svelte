<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import { DatabaseState } from '../sandbox/database-state.svelte';
	import type { PanelShell } from '../sandbox/types';
	import SqlPanel from './sql-panel.svelte';

	// one database for every story, so the wasm boot is paid once per session
	// and each story shows what the real Postgres answers
	const databaseState = new DatabaseState();

	const messageLog: PanelShell = {
		table: 'message_log_1',
		sql: 'SELECT id, payload\nFROM message_log_1\nORDER BY id DESC;',
		columns: ['id', 'payload'],
		rows: [
			['2', '{"order_id": 43, "amount_cents": 250}'],
			['1', '{"order_id": 42, "amount_cents": 1999}'],
		],
	};

	const cursors: PanelShell = {
		table: 'cursor_1',
		sql: 'SELECT g.name, c.claimed\nFROM cursor_1 c\nJOIN consumer_group g\n  ON g.id = c.consumer_group_id;',
		columns: ['name', 'claimed'],
		rows: [],
	};

	const missingTable: PanelShell = {
		table: 'message_log_2',
		sql: 'SELECT id\nFROM message_log_2;',
		columns: [],
		rows: [],
	};

	const { Story } = defineMeta({
		title: 'Board/SqlPanel',
		component: SqlPanel,
		args: {
			databaseState,
			panelShell: messageLog,
			editable: false,
		},
	});
</script>

<Story name="Rows" />
<Story name="Editable" args={{ editable: true }} />
<Story name="No rows" args={{ panelShell: cursors }} />
<Story name="Query error" args={{ panelShell: missingTable }} />
