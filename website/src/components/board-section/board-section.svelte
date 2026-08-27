<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = {
		title: string;
		columnLabels: { threadCount: string | null; lastPost: string } | null;
		children: Snippet;
	};

	let { title, columnLabels, children }: Props = $props();
</script>

<section class="board-section">
	{#if columnLabels !== null}
		<header class="band" data-columns="true">
			<span></span>
			<h2 class="band-title">{title}</h2>
			{#if columnLabels.threadCount !== null}
				<span class="column-label" data-align="center">{columnLabels.threadCount}</span>
			{:else}
				<span></span>
			{/if}
			<span class="column-label">{columnLabels.lastPost}</span>
		</header>
	{:else}
		<header class="band">
			<h2 class="band-title">{title}</h2>
		</header>
	{/if}
	{@render children()}
</section>

<style src="./board-section.css"></style>
