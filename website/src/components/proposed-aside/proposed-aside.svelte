<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = {
		title: string;
		children: Snippet;
	};

	let { title, children }: Props = $props();
</script>

<aside class="proposed-aside">
	<div class="aside-header">
		<span class="proposed-chip">PROPOSED</span>
		<span class="aside-title">{title}</span>
	</div>
	<div class="aside-body">
		{@render children()}
	</div>
</aside>

<style src="./proposed-aside.css"></style>
