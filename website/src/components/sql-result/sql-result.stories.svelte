<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import SqlResult from './sql-result.svelte';

	const { Story } = defineMeta({
		title: 'Board/SqlResult',
		component: SqlResult,
	});
</script>

<Story
	name="Shell rows"
	args={{
		result: {
			columns: ['id', 'routing_key', 'payload'],
			rows: [
				['2', null, '{"order_id": 43, "amount_cents": 250}'],
				['1', 'orders.eu.created', '{"order_id": 42, "amount_cents": 1999}'],
			],
			affectedRows: null,
			durationMs: null,
			statementCount: 1,
		},
	}}
/>
<Story
	name="Ran with timing"
	args={{
		result: {
			columns: ['id', 'routing_key', 'payload'],
			rows: [
				['2', null, '{"order_id": 43, "amount_cents": 250}'],
				['1', 'orders.eu.created', '{"order_id": 42, "amount_cents": 1999}'],
			],
			affectedRows: null,
			durationMs: 4.2,
			statementCount: 1,
		},
	}}
/>
<Story
	name="Rows affected"
	args={{
		result: {
			columns: [],
			rows: [],
			affectedRows: 1,
			durationMs: 2.8,
			statementCount: 1,
		},
	}}
/>
<Story
	name="Multiple statements"
	args={{
		result: {
			columns: ['count'],
			rows: [['3']],
			affectedRows: null,
			durationMs: 6.1,
			statementCount: 2,
		},
	}}
/>
