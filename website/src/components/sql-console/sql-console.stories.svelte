<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import SqlConsole from './sql-console.svelte';

	const { Story } = defineMeta({
		title: 'Board/SqlConsole',
		component: SqlConsole,
		args: {
			label: 'your queue, selected',
			sql: 'SELECT id, routing_key, payload\nFROM message_log_1\nORDER BY id DESC;',
			columns: ['id', 'routing_key', 'payload'],
			rows: [
				['2', null, '{"order_id": 43, "amount_cents": 250}'],
				['1', 'orders.eu.created', '{"order_id": 42, "amount_cents": 1999}'],
			],
		},
	});
</script>

<Story name="Seeded result" />
<Story
	name="Single row"
	args={{
		sql: 'SELECT id, payload\nFROM message_log_1\nWHERE id = 1;',
		columns: ['id', 'payload'],
		rows: [['1', '{"order_id": 42, "amount_cents": 1999}']],
	}}
/>
