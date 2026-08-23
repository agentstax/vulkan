<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ErrorPost from './error-post.svelte';
	import CopyButton from '../copy-button/copy-button.svelte';

	const { Story } = defineMeta({
		title: 'Board/ErrorPost',
		component: ErrorPost,
		args: {
			code: 'VK0005',
			rank: 'Permanent error',
			reportHref: 'https://github.com/agentstax/vulkan/issues/new',
			actions: null,
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

<Story name="WithCopyLink">
	{#snippet template(args)}
		<ErrorPost {...args}>
			{#snippet actions()}
				<CopyButton label="Copy link" text="https://vulkan-5ss.pages.dev/errors/VK0005/" />
			{/snippet}
			<p>As it arrives in your log or error chain — the values are your call's own.</p>
		</ErrorPost>
	{/snippet}
</Story>
