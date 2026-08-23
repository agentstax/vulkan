<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import JumpTo from './jump-to.svelte';

	const { Story } = defineMeta({
		title: 'Board/JumpTo',
		component: JumpTo,
	});
</script>

<Story name="Jump to" />
