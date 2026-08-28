<script lang="ts">
	import type { Snippet } from 'svelte';
	import PixelExclamation from '../pixel-exclamation/pixel-exclamation.svelte';
	import PostFrame from '../post-frame/post-frame.svelte';

	type Props = {
		code: string;
		rank: string;
		reportHref: string;
		// header-strip controls beside the report link; Astro call sites pass
		// null as the attribute and the "actions" slot overrides it when set
		actions: Snippet | null;
		children: Snippet;
	};

	let { code, rank, reportHref, actions, children }: Props = $props();
</script>

{#snippet headerContents()}
	<span>the log line</span>
	<span class="header-links">
		{#if actions !== null}
			{@render actions()}
			<span>&middot;</span>
		{/if}
		<a href={reportHref}>Report this thread</a>
	</span>
{/snippet}

{#snippet authorCell()}
	<span class="code-name">{code}</span>
	<span class="code-rank">{rank}</span>
	<span class="avatar"><PixelExclamation width={44} /></span>
{/snippet}

<PostFrame
	framed={true}
	header={headerContents}
	headerTone="plain"
	{authorCell}
	authorIgnored={false}
>
	{@render children()}
</PostFrame>

<style src="./error-post.css"></style>
