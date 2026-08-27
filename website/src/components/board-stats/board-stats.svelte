<script lang="ts">
	import PixelFolder from '../pixel-folder/pixel-folder.svelte';

	type Props = {
		threadCount: number;
		codeCount: number;
		decisionRecordCount: number;
	};

	let { threadCount, codeCount, decisionRecordCount }: Props = $props();
</script>

<div class="board-stats">
	<div class="stats">
		<span class="stats-line">
			{threadCount} threads
		</span>
    <span class="stats-line">
			{codeCount} error and event codes
		</span>
    <span class="stats-line">
			{decisionRecordCount} decision records
		</span>
	</div>
	<div class="legend">
		<span class="legend-row">
			<PixelFolder updated={true} pinned={false} width={13} /> updated since your last visit
		</span>
		<span class="legend-row">
			<PixelFolder updated={false} pinned={false} width={13} /> read
		</span>
		<span class="legend-row legend-visited">purple links — already visited</span>
	</div>
</div>

<style src="./board-stats.css"></style>
