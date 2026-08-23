<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import PixelFlag from './pixel-flag.svelte';

	const { Story } = defineMeta({
		title: 'Board/PixelFlag',
		component: PixelFlag,
		args: { width: 15 },
		argTypes: { width: { control: 'number', min: 10, max: 64, step: 1 } },
	});
</script>

<Story name="Announcement size" />
