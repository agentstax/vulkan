<script lang="ts">
	import type { Snippet } from 'svelte';
	import PixelExclamation from '../pixel-exclamation/pixel-exclamation.svelte';

	type Props = {
		code: string;
		rank: string;
		reportHref: string;
		children: Snippet;
	};

	let { code, rank, reportHref, children }: Props = $props();
</script>

<div class="error-post">
	<div class="post-header" data-pagefind-ignore>
		<span>the log line</span>
		<a href={reportHref}>Report this thread</a>
	</div>
	<div class="post-columns">
		<div class="author">
			<span class="code-name">{code}</span>
			<span class="code-rank">{rank}</span>
			<span class="avatar"><PixelExclamation width={44} /></span>
		</div>
		<div class="post-body">
			{@render children()}
		</div>
	</div>
</div>

<style src="./error-post.css"></style>
