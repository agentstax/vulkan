<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ThreadPost from './thread-post.svelte';

	const { Story } = defineMeta({
		title: 'Board/ThreadPost',
		component: ThreadPost,
		args: { author: 'brandon', role: 'Site Admin', header: null, postCount: null },
	});
</script>

<Story name="Post">
	{#snippet template(args)}
		<ThreadPost {...args}>
			<p>
				Every queue makes you trust its dashboard. Vulkan is Postgres, so you can just look — the
				tables below are the ones the library creates.
			</p>
		</ThreadPost>
	{/snippet}
</Story>

<Story
	name="FramedWithHeader"
	args={{
		header: {
			kind: 'posted',
			postedDate: '2026-08-22',
			reportHref: 'https://github.com/agentstax/vulkan/issues/new',
		},
		postCount: 74,
	}}
>
	{#snippet template(args)}
		<ThreadPost {...args}>
			<p>A framed post carries its own border, the posted-date strip, and the author's count.</p>
		</ThreadPost>
	{/snippet}
</Story>

<Story
	name="AcceptedAnswer"
	args={{ header: { kind: 'accepted', postedDate: '2026-08-20' }, postCount: 74 }}
>
	{#snippet template(args)}
		<ThreadPost {...args}>
			<p><strong>register it with MessageAdmin.RegisterTopic first</strong></p>
		</ThreadPost>
	{/snippet}
</Story>
