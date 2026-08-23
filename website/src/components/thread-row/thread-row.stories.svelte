<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ThreadRow from './thread-row.svelte';

	const { Story } = defineMeta({
		title: 'Board/ThreadRow',
		component: ThreadRow,
		args: {
			index: 0,
			title: 'Ordering & Concurrency',
			href: '/concepts/ordering/',
			updated: false,
			lastUpdatedDate: '2026-08-23',
			onVisit: () => {},
		},
	});
</script>

<Story name="Read" />
<Story name="Updated" args={{ updated: true }} />
<Story
	name="Error thread"
	args={{ index: 1, title: 'topic not found [VK0005]', href: '/errors/VK0005/' }}
/>
