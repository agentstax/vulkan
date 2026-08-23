<script lang="ts">
	import type { ThreadLink } from './types';

	type Props = {
		previous: ThreadLink | null;
		next: ThreadLink | null;
	};

	let { previous, next }: Props = $props();
</script>

<nav class="prev-next" aria-label="Adjacent threads">
	{#if previous !== null}
		<a class="era-button" href={previous.href}>&lsaquo; {previous.title}</a>
	{:else}
		<span></span>
	{/if}
	{#if next !== null}
		<a class="era-button" href={next.href}>{next.title} &rsaquo;</a>
	{/if}
</nav>

<style src="./prev-next.css"></style>
