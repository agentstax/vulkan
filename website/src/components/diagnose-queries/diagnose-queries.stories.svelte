<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import DiagnoseQueries from './diagnose-queries.svelte';

	const { Story } = defineMeta({
		title: 'Board/DiagnoseQueries',
		component: DiagnoseQueries,
	});
</script>

<!-- VK0029: the ordered pair most per-topic conditions want -- is the row
     there, then what does its history say -->
<Story
	name="Two queries on per-topic tables"
	args={{
		queries: [
			{
				label: 'the delivery row the dead-lettering wrote',
				sql: 'SELECT\n\tstatus,\n\tattempts,\n\tlast_error,\n\tupdated_at\nFROM exception_queue_{topic_id}\nWHERE consumer_group_id = {group_id}\n\tAND message_id = {message_id};',
				placeholders: ['topic_id', 'group_id', 'message_id'],
			},
			{
				label: 'every attempt it made, oldest first',
				sql: 'SELECT\n\tattempt,\n\tstatus,\n\terror,\n\tattempted_at\nFROM delivery_log_{topic_id}\nWHERE consumer_group_id = {group_id}\n\tAND message_id = {message_id}\nORDER BY attempt;',
				placeholders: ['topic_id', 'group_id', 'message_id'],
			},
		],
	}}
/>

<!-- a catalog condition: one shared table, one question -->
<Story
	name="One query on a shared table"
	args={{
		queries: [
			{
				label: 'the topic rows registered under that name',
				sql: "SELECT\n\tid,\n\tname,\n\tschema_version,\n\tcreated_at\nFROM topic_config\nWHERE name = '{topic}';",
				placeholders: ['topic'],
			},
		],
	}}
/>

<!-- nothing to substitute, so the note under the queries drops out -->
<Story
	name="A query with no placeholders"
	args={{
		queries: [
			{
				label: 'the migration steps this database recorded, newest first',
				sql: 'SELECT\n\tmigration_version,\n\tmin_compatible_version,\n\tstatus,\n\tcreated_at\nFROM migration_log\nWHERE system_id IS NOT NULL\nORDER BY id DESC;',
				placeholders: [],
			},
		],
	}}
/>
