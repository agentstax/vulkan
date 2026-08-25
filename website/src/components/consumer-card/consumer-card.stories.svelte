<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ConsumerCard from './consumer-card.svelte';

	const { Story } = defineMeta({
		title: 'Board/ConsumerCard',
		component: ConsumerCard,
		args: {
			consumer: {
				name: 'consumer 1',
				group: 'billing',
				autoRun: true,
				lines: [
					{ kind: 'handled', text: '#2 {"order_id": 4002, "desc": "refund card"}', status: 'ok' },
					{ kind: 'handled', text: '#1 {"order_id": 4001, "desc": "ship pallet"}', status: 'ok' },
				],
				status: { text: 'claim (1, 2] · 1 message', tone: 'plain' },
			},
			disabled: false,
			onautorun: () => {},
			onremove: () => {},
		},
	});
</script>

<Story name="Ticked twice" />
<Story
	name="Auto-run off"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			autoRun: false,
			lines: [
				{ kind: 'handled', text: '#2 {"order_id": 4002, "desc": "refund card"}', status: 'ok' },
				{ kind: 'handled', text: '#1 {"order_id": 4001, "desc": "ship pallet"}', status: 'ok' },
			],
			status: { text: 'claim (1, 2] · 1 message', tone: 'plain' },
		},
	}}
/>
<Story
	name="Before its first run"
	args={{
		consumer: {
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
		},
	}}
/>
<Story
	name="Own group, untouched cursor"
	args={{
		consumer: {
			name: 'consumer 3',
			group: 'search',
			autoRun: true,
			lines: [
				{
					kind: 'note',
					text: 'its own cursor, still at 0 —\nits first run reads all three\nmessages billing handled.',
				},
			],
			status: { text: 'no runs yet', tone: 'plain' },
		},
	}}
/>
<Story
	name="Handler returned an error"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			autoRun: true,
			lines: [
				{ kind: 'handled', text: '#3 {"order_id": 4003, "desc": "pack crate"}', status: 'error' },
			],
			status: { text: 'claim (2, 3] · 1 message', tone: 'plain' },
		},
	}}
/>
<Story
	name="Caught up"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			autoRun: true,
			lines: [
				{ kind: 'handled', text: '#3 {"order_id": 4003, "desc": "pack crate"}', status: 'ok' },
			],
			status: { text: 'caught up · nothing to claim', tone: 'plain' },
		},
	}}
/>
<Story
	name="Range with nothing to read"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			autoRun: true,
			lines: [
				{ kind: 'handled', text: '#3 {"order_id": 4003, "desc": "pack crate"}', status: 'ok' },
			],
			status: { text: 'claim (4, 5] · 0 messages', tone: 'plain' },
		},
	}}
/>
<Story
	name="The run returned an error"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			autoRun: false,
			lines: [
				{ kind: 'handled', text: '#3 {"order_id": 4003, "desc": "pack crate"}', status: 'ok' },
			],
			status: { text: 'lease lost to another consumer', tone: 'error' },
		},
	}}
/>
<Story name="Database busy" args={{ disabled: true }} />
<Story
	name="Output past the card height"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			autoRun: true,
			lines: [
				{ kind: 'handled', text: '#8 {"order_id": 4008, "desc": "reprint label"}', status: 'ok' },
				{ kind: 'handled', text: '#7 {"order_id": 4007, "desc": "hold for pickup"}', status: 'ok' },
				{ kind: 'handled', text: '#6 {"order_id": 4006, "desc": "split shipment"}', status: 'ok' },
				{ kind: 'handled', text: '#5 {"order_id": 4005, "desc": "void invoice"}', status: 'ok' },
				{ kind: 'handled', text: '#4 {"order_id": 4004, "desc": "restock shelf"}', status: 'ok' },
				{ kind: 'handled', text: '#3 {"order_id": 4003, "desc": "pack crate"}', status: 'ok' },
				{ kind: 'handled', text: '#2 {"order_id": 4002, "desc": "refund card"}', status: 'ok' },
				{ kind: 'handled', text: '#1 {"order_id": 4001, "desc": "ship pallet"}', status: 'ok' },
			],
			status: { text: 'claim (7, 8] · 1 message', tone: 'plain' },
		},
	}}
/>
