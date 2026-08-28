<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import VersionSelect from './version-select.svelte';

	const manifest = {
		latest: 'main',
		versions: [
			{ version: 'main', url: 'https://vulkan-5ss.pages.dev' },
			{ version: 'v0-demo', url: 'https://v0-demo.vulkan-5ss.pages.dev' },
		],
	};

	const { Story } = defineMeta({
		title: 'Board/VersionSelect',
		component: VersionSelect,
		args: {
			path: '/quickstart/',
			manifest,
		},
	});
</script>

<Story name="On the latest version" args={{ version: 'main' }} />
<Story name="On an old version" args={{ version: 'v0-demo' }} />
<Story name="On a version the manifest does not list" args={{ version: 'v0-unlisted' }} />
<Story name="Manifest unreachable" args={{ version: 'main', manifest: null }} />
