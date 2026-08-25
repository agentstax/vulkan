<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import type { Consumer } from '../consumer-card/types';
	import ConsumerGrid from './consumer-grid.svelte';

	const billingOne: Consumer = {
		name: 'consumer 1',
		group: 'billing',
		autoRun: true,
		lines: [
			{ kind: 'handled', text: '#2 {"order_id": 4002, "desc": "refund card"}', status: 'ok' },
			{ kind: 'handled', text: '#1 {"order_id": 4001, "desc": "ship pallet"}', status: 'ok' },
		],
		status: { text: 'claim (1, 2] · 1 message', tone: 'plain' },
	};

	const billingTwo: Consumer = {
		name: 'consumer 2',
		group: 'billing',
		autoRun: true,
		lines: [
			{
				kind: 'note',
				text: 'same group as consumer 1.\nits next run claims (2, 3] —\nranges never overlap.',
			},
		],
		status: { text: 'no runs yet', tone: 'plain' },
	};

	const search: Consumer = {
		name: 'consumer 3',
		group: 'search',
		autoRun: false,
		lines: [
			{
				kind: 'note',
				text: 'its own cursor, still at 0 —\nits first run reads all three\nmessages billing handled.',
			},
		],
		status: { text: 'no runs yet', tone: 'plain' },
	};

	const { Story } = defineMeta({
		title: 'Board/ConsumerGrid',
		component: ConsumerGrid,
		args: {
			consumers: [billingOne, billingTwo, search],
			disabled: false,
			onautorun: () => {},
			onremove: () => {},
		},
	});
</script>

<Story name="Three across" />
<Story name="One consumer" args={{ consumers: [billingOne] }} />
<Story name="No consumers" args={{ consumers: [] }} />
<Story name="Database busy" args={{ disabled: true }} />
<Story
	name="Past one row"
	args={{
		consumers: [
			billingOne,
			billingTwo,
			search,
			{ ...billingOne, name: 'consumer 4' },
			{ ...search, name: 'consumer 5' },
		],
	}}
/>
