<script lang="ts">
	import PixelFlag from '../pixel-flag/pixel-flag.svelte';

	type Props = {
		index: number;
		title: string;
		href: string;
		date: string;
	};

	let { index, title, href, date }: Props = $props();

	const stripe = $derived(index % 2 === 0 ? 'a' : 'b');
</script>

<div class="announcement-row" data-stripe={stripe}>
	<PixelFlag width={20} />
	<a class="announcement-title" {href}>{title}</a>
	<span></span>
	<span class="announcement-date">{date}</span>
</div>

<style src="./announcement-row.css"></style>
