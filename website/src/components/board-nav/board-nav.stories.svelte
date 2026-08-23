<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import BoardNav from './board-nav.svelte';

	const { Story } = defineMeta({
		title: 'Board/BoardNav',
		component: BoardNav,
		argTypes: {
			current: {
				control: 'select',
				options: ['Board Index', 'Docs', 'Troubleshooting', 'GitHub', 'Search'],
			},
		},
	});
</script>

<Story name="On the board index" args={{ current: 'Board Index' }} />
<Story name="On a docs thread" args={{ current: 'Docs' }} />
