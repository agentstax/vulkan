<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import SearchStrip from './search-strip.svelte';

	const { Story } = defineMeta({
		title: 'Board/SearchStrip',
		component: SearchStrip,
		args: {
			exampleText: 'topic not found -- register it with MessageAdmin.RegisterTopic first [VK0005]',
		},
	});
</script>

<Story name="Strip" />
