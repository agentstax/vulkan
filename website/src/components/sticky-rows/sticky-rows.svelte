<script lang="ts">
	import StickyRow from '../sticky-row/sticky-row.svelte';
	import { readTracking } from '../../state/read-tracking.svelte';

	// a sticky's scope is its own thread, not the board it links into
	type StickyRowItem = {
		title: string;
		href: string;
		lastUpdatedDate: string;
	};

	type Props = {
		stickies: StickyRowItem[];
	};

	let { stickies }: Props = $props();
</script>

{#each stickies as sticky (sticky.href)}
	<StickyRow
		title={sticky.title}
		href={sticky.href}
		updated={readTracking.isUpdated([sticky.href], sticky.lastUpdatedDate)}
		lastUpdatedDate={sticky.lastUpdatedDate}
		onVisit={() => readTracking.recordPageVisit(sticky.href)}
	/>
{/each}
