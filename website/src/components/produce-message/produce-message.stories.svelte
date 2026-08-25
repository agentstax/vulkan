<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ProduceMessage from './produce-message.svelte';

	const { Story } = defineMeta({
		title: 'Board/ProduceMessage',
		component: ProduceMessage,
		args: {
			topic: 'orders',
			text: 'restock the ovens',
			errorMessage: null,
			disabled: false,
			ontext: () => {},
			onproduce: () => {},
		},
	});
</script>

<Story name="Filled" />
<Story name="Empty" args={{ text: '' }} />
<Story name="Producing" args={{ disabled: true }} />
<Story
	name="Produce failed"
	args={{ errorMessage: 'relation "idempotency_key_1" does not exist' }}
/>
