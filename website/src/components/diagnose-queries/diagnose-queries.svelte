<script lang="ts">
	import HighlightedSql from '../highlighted-sql/highlighted-sql.svelte';
	import ThreadAside from '../thread-aside/thread-aside.svelte';
	import type { DiagnoseQuery } from './types';

	type Props = {
		queries: DiagnoseQuery[];
	};

	let { queries }: Props = $props();

	const placeholders = $derived([...new Set(queries.flatMap((query) => query.placeholders))]);
</script>

<p>Helpful info.</p>
<ol class="query-list">
	{#each queries as query (query.label)}
		<li>
			<p class="query-label">{query.label}</p>
			<div class="query-sql"><HighlightedSql sql={query.sql} /></div>
		</li>
	{/each}
</ol>
{#if placeholders.length > 0}
	<ThreadAside label="NOTE" title="Fill in your own values">
		<p>
			Make sure to substitute
			{#each placeholders as placeholder (placeholder)}<code class="placeholder-name"
					>{placeholder}</code
				>{/each}
		</p>
	</ThreadAside>
{/if}

<style src="./diagnose-queries.css"></style>
