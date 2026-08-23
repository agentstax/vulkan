<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import PixelExclamation from './pixel-exclamation.svelte';

	const { Story } = defineMeta({
		title: 'Board/PixelExclamation',
		component: PixelExclamation,
		args: { width: 44 },
	});
</script>

<Story name="Mark" />
