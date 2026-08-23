<script lang="ts">
	import type { ExcerptSegment } from '../../helpers/excerpt-segments';

	type Props = {
		title: string;
		href: string;
		excerpt: ExcerptSegment[];
	};

	let { title, href, excerpt }: Props = $props();
</script>

<div class="search-result">
	<a class="result-title" href={href}>{title}</a>
	<p class="result-excerpt">
		{#each excerpt as segment, index (index)}{#if segment.marked}<mark>{segment.text}</mark
			>{:else}{segment.text}{/if}{/each}
	</p>
</div>

<style src="./search-result.css"></style>
