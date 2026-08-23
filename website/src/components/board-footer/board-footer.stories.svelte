<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import BoardFooter from './board-footer.svelte';

	const { Story } = defineMeta({
		title: 'Board/BoardFooter',
		component: BoardFooter,
	});
</script>

<Story name="Footer" />
