<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import BoardBanner from './board-banner.svelte';

	const { Story } = defineMeta({
		title: 'Board/BoardBanner',
		component: BoardBanner,
	});
</script>

<Story name="Banner" />
