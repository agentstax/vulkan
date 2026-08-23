<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ThreadTitleBand from './thread-title-band.svelte';

	const { Story } = defineMeta({
		title: 'Board/ThreadTitleBand',
		component: ThreadTitleBand,
		args: {
			title: 'Ordering & Concurrency',
			code: null,
			solved: false,
			editHref:
				'https://github.com/agentstax/vulkan/edit/main/website/src/content/docs/concepts/ordering.mdx',
		},
	});
</script>

<Story name="Band" />

<Story name="Solved" args={{ title: 'topic not found', code: 'VK0005', solved: true }} />
