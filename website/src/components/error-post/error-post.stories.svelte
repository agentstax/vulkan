<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ErrorPost from './error-post.svelte';

	const { Story } = defineMeta({
		title: 'Board/ErrorPost',
		component: ErrorPost,
		args: {
			code: 'VK0005',
			rank: 'Permanent error',
			reportHref: 'https://github.com/agentstax/vulkan/issues/new',
		},
	});
</script>

<Story name="Post">
	{#snippet template(args)}
		<ErrorPost {...args}>
			<p>As it arrives in your log or error chain — the values are your call's own.</p>
		</ErrorPost>
	{/snippet}
</Story>
