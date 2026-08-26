<script lang="ts">
	import { markPlaceholders } from '../../helpers/placeholders';

	type Props = {
		text: string;
		kind: 'error' | 'event' | 'metric';
	};

	let { text, kind }: Props = $props();

	// this line states the code's shape, never one reader's values -- theirs
	// stay in the paste box, which has their whole line verbatim
	const segments = $derived(markPlaceholders(text));
</script>

<pre class="log-line" data-kind={kind}>{#each segments as segment, index (index)}<span
			class="log-segment"
			data-kind={segment.kind}>{segment.text}</span
		>{/each}</pre>

<style src="./log-line.css"></style>
