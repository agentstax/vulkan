<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import AnnouncementRow from './announcement-row.svelte';

	const { Story } = defineMeta({
		title: 'Board/AnnouncementRow',
		component: AnnouncementRow,
		args: {
			index: 0,
			title: 'Migration compat gate ships — old binaries stay safe through additive releases',
			href: '/guides/migrations/',
			date: '2026-08-22',
		},
	});
</script>

<Story name="First stripe" />
<Story name="Second stripe" args={{ index: 1 }} />
