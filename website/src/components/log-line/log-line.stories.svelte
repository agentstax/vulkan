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

<!-- VK0022: the composed line carries the fix, and the fix carries blanks -->
<Story
	name="Error whose fix names the caller's values"
	args={{
		text: 'schema version is older than this build requires -- migrate the {owner_kind} schema up from {version} to {build_version} [VK0022]',
		kind: 'error',
	}}
/>
