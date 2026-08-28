<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import LogLine from './log-line.svelte';

	const { Story } = defineMeta({
		title: 'Board/LogLine',
		component: LogLine,
		args: {
			text: 'schema version is older than this build requires: version 2, build_version 3, owner_kind "topic" -- migrate the topic schema up from 2 to 3 [VK0022]',
			kind: 'error',
		},
	});
</script>

<Story name="Error" />

<Story
	name="Event"
	args={{
		text: 'level=WARN msg="messages dead-lettered" code=VK0028 topic_id=1 group_id=7',
		kind: 'event',
	}}
/>
