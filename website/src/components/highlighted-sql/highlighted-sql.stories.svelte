<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import HighlightedSql from './highlighted-sql.svelte';

	const { Story } = defineMeta({
		title: 'Board/HighlightedSql',
		component: HighlightedSql,
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
