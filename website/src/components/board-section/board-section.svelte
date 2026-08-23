<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = {
		title: string;
		columnLabels: { threadCount: string; lastPost: string } | null;
		threadCount: number | null;
		children: Snippet;
	};

	let { title, columnLabels, threadCount, children }: Props = $props();
</script>

<section class="board-section">
	{#if columnLabels !== null}
		<header class="band" data-columns="true">
			<span></span>
			<h2 class="band-title">{title}</h2>
			<span class="column-label" data-align="center">{columnLabels.threadCount}</span>
			<span class="column-label">{columnLabels.lastPost}</span>
		</header>
	{:else}
		<header class="band">
			<h2 class="band-title">{title}</h2>
			{#if threadCount !== null}
				<span class="thread-total">{threadCount} {threadCount === 1 ? 'thread' : 'threads'}</span>
			{/if}
		</header>
	{/if}
	{@render children()}
</section>

<style src="./board-section.css"></style>
