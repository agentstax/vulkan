<script lang="ts">
	import { logAttributes } from '../../helpers/placeholders';
	import IslandBoundary from '../island-boundary/island-boundary.svelte';
	import { pastedLogLine } from '../../state/pasted-log-line.svelte';

	type Props = {
		// every attribute name this thread's queries and fix substitute
		placeholders: string[];
		// the thread's composed line, shown as the shape to paste. It carries
		// example values, so pasting the line above demonstrates the fill.
		exampleText: string;
	};

	let { placeholders, exampleText }: Props = $props();

	const values = $derived(logAttributes(pastedLogLine.text, placeholders));
	const pasted = $derived(pastedLogLine.text.trim() !== '');
</script>

<IslandBoundary name="paste box">
	<div class="log-line-paste">
		<label class="prompt" for="pasted-log-line">
			Landed here from a log? Paste the line — the queries above fill with your own values.
		</label>
		<textarea
			id="pasted-log-line"
			class="entry"
			rows="2"
			spellcheck="false"
			placeholder={exampleText}
			value={pastedLogLine.text}
			oninput={(event) => pastedLogLine.set(event.currentTarget.value)}></textarea>
		<div class="readout">
			{#if !pasted}
				<span class="hint">Nothing pasted yet — the queries above show their blanks.</span>
			{:else}
				<span class="hint" data-found={values.size > 0}>
					{values.size}
					{values.size === 1 ? 'value' : 'values'} read from this line.
				</span>
				<button class="era-button" onclick={() => pastedLogLine.clear()}>Clear</button>
			{/if}
		</div>
		<p class="aside">
			Looking for a different code? <a href="/search/">Search every thread</a>.
		</p>
	</div>
</IslandBoundary>

<style src="./log-line-paste.css"></style>
