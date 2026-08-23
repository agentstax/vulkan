<script lang="ts">
	import { repositoryUrl } from '../../site';

	type NavLink = {
		label: string;
		href: string;
	};

	type Props = {
		current: string;
	};

	let { current }: Props = $props();

	// Search stays inert until the Pagefind island exists
	const links: NavLink[] = [
		{ label: 'Board Index', href: '/board/' },
		{ label: 'Docs', href: '/' },
		{ label: 'Troubleshooting', href: '/errors/' },
		{ label: 'GitHub', href: repositoryUrl },
		{ label: 'Search', href: '#' },
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
