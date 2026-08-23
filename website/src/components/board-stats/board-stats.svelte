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
			{threadCount} threads · {codeCount} error and event codes · {decisionRecordCount} decision records
		</span>
		<span class="stats-note">every number on this site is real or absent</span>
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
