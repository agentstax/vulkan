<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import PrevNext from './prev-next.svelte';

	const { Story } = defineMeta({
		title: 'Board/PrevNext',
		component: PrevNext,
		args: {
			previous: { title: 'Routing', href: '/concepts/routing/' },
			next: { title: 'Ordering & Concurrency', href: '/concepts/ordering/' },
		},
	});
</script>

<Story name="Both" />

<Story name="LastThread" args={{ next: null }} />

<Story name="FirstThread" args={{ previous: null }} />
