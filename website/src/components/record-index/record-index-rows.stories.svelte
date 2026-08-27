<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import RecordIndexRows from './record-index-rows.svelte';

	const { Story } = defineMeta({
		title: 'Board/RecordIndexRows',
		component: RecordIndexRows,
		args: {
			rows: [
				{
					number: '0590',
					href: '/decisions/0590/',
					date: '2026-08-26',
					status: 'accepted',
					title: "a fix substitutes the caller's values",
				},
				{
					number: '0591',
					href: '/decisions/0591/',
					date: '2026-08-26',
					status: 'rejected',
					title: "the sandbox's Postgres is not prefetched",
				},
				{
					number: '0519',
					href: '/decisions/0519/',
					date: '2026-08-15',
					status: 'superseded',
					title: 'Topic config lives in append-only declaration rows; topic keeps identity only',
				},
			],
		},
	});
</script>

<Story name="Every status" />
