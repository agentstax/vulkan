<script lang="ts">
	import PixelFolder from '../pixel-folder/pixel-folder.svelte';

	type Props = {
		title: string;
		href: string;
		updated: boolean;
		lastUpdatedDate: string;
	};

	let { title, href, updated, lastUpdatedDate }: Props = $props();
</script>

<div class="sticky-row">
	<PixelFolder {updated} pinned={true} width={19} />
	<div class="sticky-name">
		<span class="sticky-label">STICKY</span>
		<a class="sticky-title" {href}>{title}</a>
	</div>
	<span></span>
	<span class="sticky-date">updated {lastUpdatedDate}</span>
</div>

<style src="./sticky-row.css"></style>
