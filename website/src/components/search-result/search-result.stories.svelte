<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import SearchResult from './search-result.svelte';

	const { Story } = defineMeta({
		title: 'Board/SearchResult',
		component: SearchResult,
		args: {
			title: 'topic not found [VK0005]',
			href: '/errors/VK0005/',
			excerpt: [
				{ text: 'the ', marked: false },
				{ text: 'topic', marked: true },
				{ text: ' named in the call is not registered — register it with ', marked: false },
				{ text: 'MessageAdmin.RegisterTopic', marked: true },
				{ text: ' first.', marked: false },
			],
		},
	});
</script>

<Story name="Result" />
