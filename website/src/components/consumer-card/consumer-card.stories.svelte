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
					{ kind: 'claim', text: 'claim (0, 1]' },
					{ kind: 'handled', text: '"ship order 4471"', status: 'ok' },
					{ kind: 'claim', text: 'claim (1, 2]' },
					{ kind: 'handled', text: '"refund order 4468"', status: 'ok' },
				],
			},
			ontick: () => {},
			onremove: () => {},
		},
	});
</script>

<Story name="Ticked twice" />
<Story
	name="Sharing a group"
	args={{
		consumer: {
			name: 'consumer 2',
			group: 'billing',
			lines: [
				{
					kind: 'note',
					text: 'same group as consumer 1.\nits next tick claims (2, 3] —\nranges never overlap.',
				},
			],
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
					text: 'its own cursor, still at 0 —\nits first tick reads all three\nmessages billing handled.',
				},
			],
		},
	}}
/>
<Story
	name="Handler returned an error"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			lines: [
				{ kind: 'claim', text: 'claim (2, 3]' },
				{ kind: 'handled', text: '"restock the ovens"', status: 'error' },
			],
		},
	}}
/>
<Story
	name="Output past the card height"
	args={{
		consumer: {
			name: 'consumer 1',
			group: 'billing',
			lines: [
				{ kind: 'claim', text: 'claim (0, 1]' },
				{ kind: 'handled', text: '"ship order 4471"', status: 'ok' },
				{ kind: 'claim', text: 'claim (1, 2]' },
				{ kind: 'handled', text: '"refund order 4468"', status: 'ok' },
				{ kind: 'claim', text: 'claim (2, 3]' },
				{ kind: 'handled', text: '"restock the ovens"', status: 'ok' },
				{ kind: 'claim', text: 'claim (3, 4]' },
				{ kind: 'handled', text: '"ship order 4472"', status: 'ok' },
			],
		},
	}}
/>
