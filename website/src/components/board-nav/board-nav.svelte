<script lang="ts">
	import { repositoryUrl } from '../../site';

	type NavLink = {
		label: string;
		href: string;
	};

	type Props = {
		// null on a page no nav link names (a thread, the 404 page)
		current: string | null;
	};

	let { current }: Props = $props();

	const links: NavLink[] = [
		{ label: 'Board Index', href: '/' },
		{ label: 'Troubleshooting', href: '/errors/' },
		{ label: 'Search', href: '/search/' },
		{ label: 'GitHub', href: repositoryUrl },
	];
</script>

<nav class="board-nav" aria-label="Board">
	{#each links as link (link.label)}
		<a href={link.href} aria-current={link.label === current ? 'page' : undefined}>
			{link.label}
		</a>
	{/each}
</nav>

<style src="./board-nav.css"></style>
