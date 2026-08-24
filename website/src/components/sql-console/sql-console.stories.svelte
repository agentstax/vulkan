<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import SqlConsole from './sql-console.svelte';

	const { Story } = defineMeta({
		title: 'Board/SqlConsole',
		component: SqlConsole,
		args: {
			label: 'your queue, selected',
			topic: 'orders',
			messages: {
				table: 'message_log_1',
				sql: 'SELECT id, routing_key, payload\nFROM message_log_1\nORDER BY id DESC;',
				columns: ['id', 'routing_key', 'payload'],
				rows: [
					['2', null, '{"order_id": 43, "amount_cents": 250}'],
					['1', 'orders.eu.created', '{"order_id": 42, "amount_cents": 1999}'],
				],
			},
			cursors: {
				table: 'cursor_1',
				sql: 'SELECT g.name, c.claimed\nFROM cursor_1 c\nJOIN consumer_group g\n  ON g.id = c.consumer_group_id;',
				columns: ['name', 'claimed'],
				rows: [],
			},
		},
	});
</script>

<Story name="Seeded result" />
<Story
	name="Groups declared"
	args={{
		cursors: {
			table: 'cursor_1',
			sql: 'SELECT g.name, c.claimed\nFROM cursor_1 c\nJOIN consumer_group g\n  ON g.id = c.consumer_group_id;',
			columns: ['name', 'claimed'],
			rows: [
				['billing', '2'],
				['search', '0'],
			],
		},
	}}
/>
