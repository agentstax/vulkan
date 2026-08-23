<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import StickyRow from './sticky-row.svelte';

	const { Story } = defineMeta({
		title: 'Board/StickyRow',
		component: StickyRow,
		args: {
			title: 'Quickstart',
			href: '/quickstart/',
			updated: false,
			lastUpdatedDate: '2026-08-22',
			onVisit: () => {},
		},
	});
</script>

<Story name="Read" />
<Story name="Updated since last visit" args={{ updated: true }} />
