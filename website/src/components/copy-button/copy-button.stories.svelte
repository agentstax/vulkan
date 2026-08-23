<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import CopyButton from './copy-button.svelte';

	const { Story } = defineMeta({
		title: 'Board/CopyButton',
		component: CopyButton,
		args: {
			label: 'Copy link',
			text: 'https://vulkan-5ss.pages.dev/concepts/ordering/',
		},
	});
</script>

<Story name="Copy link" />
