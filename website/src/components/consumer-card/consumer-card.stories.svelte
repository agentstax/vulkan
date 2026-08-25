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
				lines: [
					{ kind: 'handled', text: '#2 "refund order 4468"', status: 'ok' },
					{ kind: 'handled', text: '#1 "ship order 4471"', status: 'ok' },
				],
				status: { text: 'claim (1, 2] · 1 message', tone: 'plain' },
			},
			disabled: false,
			ontick: () => {},
			onremove: () => {},
		},
	});
</script>

<Story name="Ticked twice" />
<Story
	name="Before its first run"
	args={{
		consumer: {
			name: 'consumer 2',
			group: 'billing',
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
			lines: [{ kind: 'handled', text: '#3 "restock the ovens"', status: 'error' }],
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
			lines: [{ kind: 'handled', text: '#3 "restock the ovens"', status: 'ok' }],
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
			lines: [{ kind: 'handled', text: '#3 "restock the ovens"', status: 'ok' }],
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
			lines: [{ kind: 'handled', text: '#3 "restock the ovens"', status: 'ok' }],
			status: { text: 'lease lost to another consumer', tone: 'error' },
		},
	}}
/>
<Story name="Tick in flight" args={{ disabled: true }} />
<Story
	name="Output past the card height"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			lines: [
				{ kind: 'handled', text: '#8 "ship order 4475"', status: 'ok' },
				{ kind: 'handled', text: '#7 "refund order 4474"', status: 'ok' },
				{ kind: 'handled', text: '#6 "ship order 4473"', status: 'ok' },
				{ kind: 'handled', text: '#5 "restock the ovens"', status: 'ok' },
				{ kind: 'handled', text: '#4 "ship order 4472"', status: 'ok' },
				{ kind: 'handled', text: '#3 "restock the ovens"', status: 'ok' },
				{ kind: 'handled', text: '#2 "refund order 4468"', status: 'ok' },
				{ kind: 'handled', text: '#1 "ship order 4471"', status: 'ok' },
			],
			status: { text: 'claim (7, 8] · 1 message', tone: 'plain' },
		},
	}}
/>
