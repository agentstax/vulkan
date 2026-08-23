<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = {
		label: string;
		title: string;
		children: Snippet;
	};

	let { label, title, children }: Props = $props();
</script>

<aside class="thread-aside">
	<div class="aside-header">
		<span class="aside-chip">{label}</span>
		<span class="aside-title">{title}</span>
	</div>
	<div class="aside-body">
		{@render children()}
	</div>
</aside>

<style src="./thread-aside.css"></style>
