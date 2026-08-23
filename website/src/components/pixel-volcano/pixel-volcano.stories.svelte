<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import PixelVolcano from './pixel-volcano.svelte';

	const { Story } = defineMeta({
		title: 'Board/PixelVolcano',
		component: PixelVolcano,
		args: { width: 76 },
		argTypes: { width: { control: 'number', min: 16, max: 128, step: 1 } },
	});
</script>

<Story name="Banner size" />
<Story name="Favicon size" args={{ width: 32 }} />
