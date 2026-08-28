<script lang="ts">
	import type { Snippet } from 'svelte';
	import PixelVolcano from '../pixel-volcano/pixel-volcano.svelte';
	import PostFrame from '../post-frame/post-frame.svelte';
	import type { PostHeader } from './types';

	type Props = {
		author: string;
		role: string;
		header: PostHeader | null;
		postCount: number | null;
		// header-strip controls beside the report link; Astro call sites pass
		// null as the attribute and the "actions" slot overrides it when set
		actions: Snippet | null;
		children: Snippet;
	};

	let { author, role, header, postCount, actions, children }: Props = $props();
</script>

{#snippet headerContents()}
	{#if header !== null}
		{#if header.kind === 'accepted'}
			<span class="accepted-label"><span class="accepted-mark"></span>ACCEPTED ANSWER</span>
			<span class="posted">Posted: {header.postedDate}</span>
		{:else}
			<span class="posted">Posted: {header.postedDate}</span>
			<span class="header-links">
				{#if actions !== null}
					{@render actions()}
					<span>&middot;</span>
				{/if}
				<a href={header.reportHref}>Report this thread</a>
			</span>
		{/if}
	{/if}
{/snippet}

{#snippet authorCell()}
	<a class="author-name" href={`/members/${author}/`}>{author}</a>
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
	<a class="avatar" href={`/members/${author}/`}><PixelVolcano width={44} /></a>
	{#if postCount !== null}
		<span class="post-count">Posts: {postCount}</span>
	{/if}
{/snippet}

<PostFrame
	framed={header !== null}
	header={header === null ? null : headerContents}
	headerTone={header !== null && header.kind === 'accepted' ? 'accepted' : 'plain'}
	{authorCell}
	authorIgnored={true}
>
	{@render children()}
</PostFrame>

<style src="./thread-post.css"></style>
