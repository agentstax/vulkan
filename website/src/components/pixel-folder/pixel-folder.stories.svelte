<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import PixelFolder from './pixel-folder.svelte';

	const { Story } = defineMeta({
		title: 'Board/PixelFolder',
		component: PixelFolder,
		args: { updated: false, width: 18 },
		argTypes: { width: { control: 'number', min: 10, max: 64, step: 1 } },
	});
</script>

<Story name="Read" />
<Story name="Updated since last visit" args={{ updated: true }} />
<Story name="Legend size" args={{ width: 13 }} />
