<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import DatabaseProgress from './database-progress.svelte';

	const { Story } = defineMeta({
		title: 'Board/DatabaseProgress',
		component: DatabaseProgress,
	});
</script>

<Story name="Downloading" args={{ stage: 'downloading' }} />
<Story name="Starting postgres" args={{ stage: 'starting postgres' }} />
<Story name="Creating tables" args={{ stage: 'creating tables' }} />
