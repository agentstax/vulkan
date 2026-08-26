<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import HighlightedSql from './highlighted-sql.svelte';

	const { Story } = defineMeta({
		title: 'Board/HighlightedSql',
		component: HighlightedSql,
		args: {
			values: new Map<string, string>(),
		},
	});
</script>

<Story
	name="Example query"
	args={{ sql: 'SELECT id, routing_key, payload\nFROM message_log_1\nORDER BY id DESC;' }}
/>
<Story
	name="Lowercase keywords"
	args={{ sql: "select count(*) from delivery_1 where status = 'dead';" }}
/>
<Story
	name="Declared query with placeholders"
	args={{
		sql: 'SELECT status, attempts\nFROM delivery_{topic_id}\nWHERE consumer_group_id = {group_id};',
	}}
/>

<Story
	name="Placeholders filled from a log line"
	args={{
		sql: 'SELECT status, attempts\nFROM delivery_{topic_id}\nWHERE consumer_group_id = {group_id};',
		values: new Map([
			['topic_id', '1'],
			['group_id', '2'],
		]),
	}}
/>

<!-- a text literal carries its own quotes, so the value goes in raw -->
<Story
	name="Text literal filled"
	args={{
		sql: "SELECT id, name FROM topic\nWHERE name = '{topic}';",
		values: new Map([['topic', 'orders']]),
	}}
/>
