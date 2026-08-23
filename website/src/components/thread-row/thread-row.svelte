<script lang="ts">
	import PixelFolder from '../pixel-folder/pixel-folder.svelte';

	type Props = {
		index: number;
		title: string;
		href: string;
		updated: boolean;
		lastUpdatedDate: string;
		onVisit: () => void;
	};

	let { index, title, href, updated, lastUpdatedDate, onVisit }: Props = $props();
</script>

<div class="thread-row" data-stripe={index % 2 === 0 ? 'a' : 'b'}>
	<PixelFolder {updated} pinned={false} width={19} />
	<a class="thread-row-title" {href} onclick={onVisit}>{title}</a>
	<span></span>
	<span class="thread-row-date">updated {lastUpdatedDate}</span>
</div>

<style src="./thread-row.css"></style>
