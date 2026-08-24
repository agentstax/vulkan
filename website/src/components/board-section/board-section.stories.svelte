<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import BoardSection from './board-section.svelte';
	import BoardRow from '../board-row/board-row.svelte';
	import StickyRow from '../sticky-row/sticky-row.svelte';

	const { Story } = defineMeta({
		title: 'Board/BoardSection',
		component: BoardSection,
		args: { title: 'Board', columnLabels: null, threadCount: null },
	});
</script>

<Story
	name="With column labels"
	args={{ columnLabels: { threadCount: 'Threads', lastPost: 'Last post' } }}
>
	{#snippet template(args)}
		{@const { children: _children, ...storyProps } = args}
		<BoardSection {...storyProps}>
			<BoardRow
				index={0}
				title="Concepts"
				href="/concepts/queue-and-log/"
				description="queue & log, lifecycle, ordering, routing, fan-out, architecture"
				threadCount={6}
				lastPostTitle="Ordering & Concurrency"
				lastPostHref="/concepts/ordering/"
				lastPostDate="2026-08-22"
				updated
				onVisit={() => {}}
			/>
			<BoardRow
				index={1}
				title="Guides"
				href="/guides/transactional-produce/"
				description="transactional produce, dead letters, replay, migrations"
				threadCount={4}
				lastPostTitle="Transactional Produce"
				lastPostHref="/guides/transactional-produce/"
				lastPostDate="2026-08-22"
				updated={false}
				onVisit={() => {}}
			/>
		</BoardSection>
	{/snippet}
</Story>
<Story name="With thread total" args={{ title: 'Start Here', threadCount: 2 }}>
	{#snippet template(args)}
		{@const { children: _children, ...storyProps } = args}
		<BoardSection {...storyProps}>
			<StickyRow
				title="Quickstart"
				href="/quickstart/"
				updated={false}
				lastUpdatedDate="2026-08-22"
				onVisit={() => {}}
			/>
			<StickyRow
				title="Why Vulkan"
				href="/why-vulkan/"
				updated={false}
				lastUpdatedDate="2026-08-22"
				onVisit={() => {}}
			/>
		</BoardSection>
	{/snippet}
</Story>
<Story name="Title only" args={{ title: 'Announcements' }}>
	{#snippet template(args)}
		{@const { children: _children, ...storyProps } = args}
		<BoardSection {...storyProps}>
			<div style="padding: 12px 14px; font-size: 12px;">rows go here</div>
		</BoardSection>
	{/snippet}
</Story>
