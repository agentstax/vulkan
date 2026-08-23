<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import BoardRow from './board-row.svelte';

	const { Story } = defineMeta({
		title: 'Board/BoardRow',
		component: BoardRow,
		args: {
			index: 0,
			title: 'Concepts',
			href: '/concepts/queue-and-log/',
			description: 'queue & log, lifecycle, ordering, routing, fan-out, architecture',
			threadCount: 6,
			lastPostTitle: 'Ordering & Concurrency',
			lastPostHref: '/concepts/ordering/',
			lastPostDate: '2026-08-22',
		},
		argTypes: { index: { control: 'number', min: 0, step: 1 } },
	});
</script>

<Story name="Read" />
<Story name="Updated since last visit" args={{ updated: true }} />
<Story name="Second stripe" args={{ index: 1 }} />
<Story
	name="Long description"
	args={{
		title: 'Troubleshooting',
		description:
			'every VK error code and log event, one thread each — the problem line is the title, so pasting a message into search lands here',
		threadCount: 52,
	}}
/>
