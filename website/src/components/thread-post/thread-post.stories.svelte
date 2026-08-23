<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ThreadPost from './thread-post.svelte';

	const { Story } = defineMeta({
		title: 'Board/ThreadPost',
		component: ThreadPost,
		args: { author: 'brandon', role: 'Site Admin' },
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
