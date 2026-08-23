<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ConsoleProgress from './console-progress.svelte';

	const { Story } = defineMeta({
		title: 'Board/ConsoleProgress',
		component: ConsoleProgress,
	});
</script>

<Story name="Downloading" args={{ stage: 'downloading' }} />
<Story name="Starting postgres" args={{ stage: 'starting postgres' }} />
<Story name="Creating tables" args={{ stage: 'creating tables' }} />
