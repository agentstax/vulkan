<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import Sandbox from './sandbox.svelte';

	const { Story } = defineMeta({
		title: 'Board/Sandbox',
		component: Sandbox,
		args: {
			topic: 'orders',
			messages: {
				table: 'message_log_1',
				sql: 'SELECT id, payload\nFROM message_log_1\nORDER BY id DESC;',
				columns: ['id', 'payload'],
				rows: [
					['2', '{"order_id": 43, "amount_cents": 250}'],
					['1', '{"order_id": 42, "amount_cents": 1999}'],
				],
			},
			cursors: {
				table: 'consumer_group_cursor_1',
				sql: 'SELECT g.name, c.claimed\nFROM consumer_group_cursor_1 c\nJOIN consumer_group_config g\n  ON g.id = c.consumer_group_id;',
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
			table: 'consumer_group_cursor_1',
			sql: 'SELECT g.name, c.claimed\nFROM consumer_group_cursor_1 c\nJOIN consumer_group_config g\n  ON g.id = c.consumer_group_id;',
			columns: ['name', 'claimed'],
			rows: [
				['billing', '2'],
				['search', '0'],
			],
		},
	}}
/>
