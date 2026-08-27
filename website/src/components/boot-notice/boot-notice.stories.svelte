<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import BootNotice from './boot-notice.svelte';

	const { Story } = defineMeta({
		title: 'Board/BootNotice',
		component: BootNotice,
	});
</script>

<Story name="Could not start" />
