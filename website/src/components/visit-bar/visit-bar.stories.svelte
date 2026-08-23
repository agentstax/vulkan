<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import VisitBar from './visit-bar.svelte';

	const { Story } = defineMeta({
		title: 'Board/VisitBar',
		component: VisitBar,
	});
</script>

<Story name="First visit" args={{ lastVisitDate: null }} />
<Story name="Returning visitor" args={{ lastVisitDate: '2026-08-22, 18:12' }} />
