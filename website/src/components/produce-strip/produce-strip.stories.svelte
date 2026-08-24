<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ProduceStrip from './produce-strip.svelte';

	const { Story } = defineMeta({
		title: 'Board/ProduceStrip',
		component: ProduceStrip,
		args: {
			topic: 'orders',
			text: 'restock the ovens',
			ontext: () => {},
		},
	});
</script>

<Story name="Filled" />
<Story name="Empty" args={{ text: '' }} />
