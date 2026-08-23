<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import LogLine from './log-line.svelte';

	const { Story } = defineMeta({
		title: 'Board/LogLine',
		component: LogLine,
		args: {
			text: 'topic not found -- register it with MessageAdmin.RegisterTopic first [VK0005]',
			kind: 'error',
		},
	});
</script>

<Story name="Error" />

<Story
	name="Event"
	args={{ text: 'level=WARN msg="messages dead-lettered" code=VK0028', kind: 'event' }}
/>
