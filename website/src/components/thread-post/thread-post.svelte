<script lang="ts">
	import type { Snippet } from 'svelte';
	import PixelVolcano from '../pixel-volcano/pixel-volcano.svelte';

	type Props = {
		author: string;
		role: string;
		children: Snippet;
	};

	let { author, role, children }: Props = $props();
</script>

<div class="thread-post">
	<div class="author">
		<span class="author-name">{author}</span>
		<span class="stars">
			{#each [0, 1, 2] as star (star)}
				<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
					<path
						d="M5 0 L6.3 3.4 L10 3.6 L7.1 5.9 L8.1 9.5 L5 7.4 L1.9 9.5 L2.9 5.9 L0 3.6 L3.7 3.4 z"
					/>
				</svg>
			{/each}
		</span>
		<span class="author-role">{role}</span>
		<span class="avatar"><PixelVolcano width={44} /></span>
	</div>
	<div class="post-body">
		{@render children()}
	</div>
</div>

<style src="./thread-post.css"></style>
