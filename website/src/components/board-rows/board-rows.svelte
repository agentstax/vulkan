<script lang="ts">
	import BoardRow from '../board-row/board-row.svelte';
	import { readTracking } from '../../state/read-tracking.svelte';

	type BoardRowItem = {
		title: string;
		href: string;
		description: string;
		threadCount: number;
		lastPostTitle: string;
		lastPostHref: string;
		lastPostDate: string;
		scopeHrefs: string[];
	};

	type Props = {
		rows: BoardRowItem[];
	};

	let { rows }: Props = $props();
</script>

{#each rows as row, index (row.href)}
	<BoardRow
		{index}
		title={row.title}
		href={row.href}
		description={row.description}
		threadCount={row.threadCount}
		lastPostTitle={row.lastPostTitle}
		lastPostHref={row.lastPostHref}
		lastPostDate={row.lastPostDate}
		updated={readTracking.isUpdated(row.scopeHrefs, row.lastPostDate)}
		onVisit={(visitedHref) => readTracking.recordPageVisit(visitedHref)}
	/>
{/each}
