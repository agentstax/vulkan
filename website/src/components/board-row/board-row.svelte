<script lang="ts">
	import PixelFolder from '../pixel-folder/pixel-folder.svelte';

	type Props = {
		index: number;
		title: string;
		href: string;
		description: string;
		threadCount: number;
		lastPostTitle: string;
		lastPostHref: string;
		lastPostDate: string;
		updated: boolean;
	};

	let {
		index,
		title,
		href,
		description,
		threadCount,
		lastPostTitle,
		lastPostHref,
		lastPostDate,
		updated,
	}: Props = $props();

	const stripe = $derived(index % 2 === 0 ? 'a' : 'b');
</script>

<div class="board-row" data-stripe={stripe}>
	<PixelFolder {updated} width={18} />
	<div class="board-name">
		<a class="board-title" {href}>{title}</a>
		<span class="board-description">{description}</span>
	</div>
	<span class="thread-count">{threadCount}</span>
	<div class="last-post">
		<a href={lastPostHref}>{lastPostTitle}</a>
		<span class="last-post-date">{lastPostDate}</span>
	</div>
</div>

<style src="./board-row.css"></style>
