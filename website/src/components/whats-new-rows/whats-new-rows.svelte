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

	const changedRows = $derived(
		rows.filter((row) => readTracking.isUpdated([row.href], row.lastUpdatedDate)),
	);
</script>

{#if changedRows.length === 0}
	<p class="whats-new-status">nothing has changed since your last visit</p>
{:else}
	<p class="whats-new-status">
		{changedRows.length}
		{changedRows.length === 1 ? 'thread has' : 'threads have'} changed since your last visit
	</p>
	{#each changedRows as row, index (row.href)}
		<ThreadRow
			{index}
			title={row.title}
			href={row.href}
			updated={true}
			lastUpdatedDate={row.lastUpdatedDate}
			onVisit={() => readTracking.recordPageVisit(row.href)}
		/>
	{/each}
{/if}

<style src="./whats-new-rows.css"></style>
