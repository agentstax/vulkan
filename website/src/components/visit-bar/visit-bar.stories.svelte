<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import VisitBar from './visit-bar.svelte';

	const manifest = {
		latest: 'main',
		versions: [
			{ version: 'main', url: 'https://vulkan-5ss.pages.dev' },
			{ version: 'v0-demo', url: 'https://v0-demo.vulkan-5ss.pages.dev' },
		],
	};

	const { Story } = defineMeta({
		title: 'Board/VisitBar',
		component: VisitBar,
		args: {
			lastVisitDate: '2026-08-22',
			version: 'main',
			manifest,
			path: '/quickstart/',
		},
	});
</script>

<Story name="First visit" args={{ lastVisitDate: null }} />
<Story name="Prerendered placeholder" args={{ lastVisitDate: '9999-99-99' }} />
<Story name="Returning visitor" />
<Story name="On an old version" args={{ version: 'v0-demo' }} />
