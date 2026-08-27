<script lang="ts">
	import { fillText, logAttributes } from '../../helpers/placeholders';
	import IslandBoundary from '../island-boundary/island-boundary.svelte';
	import { pastedLogLine } from '../../state/pasted-log-line.svelte';

	type Props = {
		fix: string;
		// the names the declaration substitutes, from codes.json
		placeholders: string[];
	};

	let { fix, placeholders }: Props = $props();

	const values = $derived(logAttributes(pastedLogLine.text, placeholders));
	const segments = $derived(fillText(fix, values));
</script>

<IslandBoundary name="fix line">
	<p class="fix-line">
		{#each segments as segment, index (index)}<span class="fix-segment" data-kind={segment.kind}
				>{segment.text}</span
			>{/each}
	</p>
</IslandBoundary>

<style src="./fix-line.css"></style>
