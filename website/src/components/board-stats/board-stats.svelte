<script lang="ts">
	import PixelFolder from '../pixel-folder/pixel-folder.svelte';

	type Props = {
		docCount: number;
		codeCount: number;
		decisionRecordCount: number;
	};

	let { docCount, codeCount, decisionRecordCount }: Props = $props();
</script>

<div class="board-stats">
	<div class="stats">
		<span class="stats-line">
			<a href="/whats-new/">{docCount} documents</a>
		</span>
		<span class="stats-line">
			<a href="/errors/">{codeCount} error and event codes</a>
		</span>
		<span class="stats-line">
			<a href="/decisions/">{decisionRecordCount} decision records</a>
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
