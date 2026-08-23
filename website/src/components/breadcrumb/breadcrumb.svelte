<script lang="ts">
	type BreadcrumbItem = {
		label: string;
		href: string;
	};

	type Props = {
		trail: BreadcrumbItem[];
	};

	let { trail }: Props = $props();
</script>

<nav class="breadcrumb" aria-label="Breadcrumb">
	{#each trail as item, index (item.href)}
		{#if index > 0}<span class="separator">»</span>{/if}
		<a href={item.href}>{item.label}</a>
	{/each}
</nav>

<style src="./breadcrumb.css"></style>
