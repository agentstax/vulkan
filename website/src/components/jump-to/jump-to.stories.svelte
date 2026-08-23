<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import JumpTo from './jump-to.svelte';

	const { Story } = defineMeta({
		title: 'Board/JumpTo',
		component: JumpTo,
		args: {
			targets: [
				{ label: 'Getting Started', href: '/quickstart/' },
				{ label: 'Concepts', href: '/concepts/queue-and-log/' },
				{ label: 'Troubleshooting', href: '/errors/' },
			],
		},
	});
</script>

<Story name="Jump to" />
