<script lang="ts">
	type BreadcrumbItem = {
		label: string;
		// null = the page the trail ends on, rendered as plain text
		href: string | null;
	};

	type Props = {
		trail: BreadcrumbItem[];
	};

	let { trail }: Props = $props();
</script>

<nav class="breadcrumb" aria-label="Breadcrumb">
	{#each trail as item, index (item.label)}
		{#if index > 0}<span class="separator">»</span>{/if}
		{#if item.href === null}
			<span class="current">{item.label}</span>
		{:else}
			<a href={item.href}>{item.label}</a>
		{/if}
	{/each}
</nav>

<style src="./breadcrumb.css"></style>
