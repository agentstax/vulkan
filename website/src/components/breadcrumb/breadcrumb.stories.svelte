<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import Breadcrumb from './breadcrumb.svelte';

	const { Story } = defineMeta({
		title: 'Board/Breadcrumb',
		component: Breadcrumb,
	});
</script>

<Story name="Board index" args={{ trail: [{ label: 'Board Index', href: '/board/' }] }} />
<Story
	name="Thread trail"
	args={{
		trail: [
			{ label: 'Board Index', href: '/board/' },
			{ label: 'Concepts', href: '/concepts/queue-and-log/' },
			{ label: 'Ordering & Concurrency', href: '/concepts/ordering/' },
		],
	}}
/>
