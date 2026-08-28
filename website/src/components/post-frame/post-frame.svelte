<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = {
		// full border standing alone; border-top when stacked in a section
		framed: boolean;
		// contents of the chrome strip above the columns; null drops the strip
		header: Snippet | null;
		// 'accepted' tints the strip with the solved colors
		headerTone: 'plain' | 'accepted';
		// contents of the left author cell
		authorCell: Snippet;
		// sets data-pagefind-ignore on the author cell: board furniture on a
		// thread post, indexable content (the code) on an error thread
		authorIgnored: boolean;
		children: Snippet;
	};

	let { framed, header, headerTone, authorCell, authorIgnored, children }: Props = $props();
</script>

<div class="post-frame" data-framed={framed}>
	{#if header !== null}
		<div class="post-header" data-tone={headerTone} data-pagefind-ignore>
			{@render header()}
		</div>
	{/if}
	<div class="post-columns">
		<div class="author" data-pagefind-ignore={authorIgnored ? '' : null}>
			{@render authorCell()}
		</div>
		<div class="post-body">
			{@render children()}
		</div>
	</div>
</div>

<style src="./post-frame.css"></style>
