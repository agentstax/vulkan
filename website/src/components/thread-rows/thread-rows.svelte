<script lang="ts">
	import ThreadRow from '../thread-row/thread-row.svelte';
	import { readTracking } from '../../state/read-tracking.svelte';

	// a listed thread's scope is the thread itself
	type ThreadRowItem = {
		title: string;
		href: string;
		lastUpdatedDate: string;
	};

	type Props = {
		rows: ThreadRowItem[];
	};

	let { rows }: Props = $props();
</script>

{#each rows as row, index (row.href)}
	<ThreadRow
		{index}
		title={row.title}
		href={row.href}
		updated={readTracking.isUpdated([row.href], row.lastUpdatedDate)}
		lastUpdatedDate={row.lastUpdatedDate}
		onVisit={() => readTracking.recordPageVisit(row.href)}
	/>
{/each}
