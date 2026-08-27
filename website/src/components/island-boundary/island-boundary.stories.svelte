<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import IslandBoundary from './island-boundary.svelte';

	const { Story } = defineMeta({
		title: 'Board/IslandBoundary',
		component: IslandBoundary,
		args: { name: 'sandbox' },
	});

	// the stand-in for a section with a defect: the throw lands while the
	// boundary's children render, which is exactly what it catches
	function throwOnRender(): string {
		throw new Error('relation "delivery" does not exist');
	}
</script>

<Story name="Content intact">
	{#snippet template(args)}
		{@const { children: _children, ...storyProps } = args}
		<IslandBoundary {...storyProps}>
			<p>the section's own content renders untouched</p>
		</IslandBoundary>
	{/snippet}
</Story>
<Story name="Failed">
	{#snippet template(args)}
		{@const { children: _children, ...storyProps } = args}
		<IslandBoundary {...storyProps}>
			<p>{throwOnRender()}</p>
		</IslandBoundary>
	{/snippet}
</Story>
