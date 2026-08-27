<script lang="ts">
	import HighlightedSql from '../highlighted-sql/highlighted-sql.svelte';
	import CopyButton from '../copy-button/copy-button.svelte';
	import IslandBoundary from '../island-boundary/island-boundary.svelte';
	import ThreadAside from '../thread-aside/thread-aside.svelte';
	import { filledSql } from '../highlighted-sql/highlight';
	import { logAttributes } from '../../helpers/placeholders';
	import { pastedLogLine } from '../../state/pasted-log-line.svelte';
	import type { DiagnoseQuery } from './types';

	type Props = {
		queries: DiagnoseQuery[];
	};

	let { queries }: Props = $props();

	const placeholders = $derived([...new Set(queries.flatMap((query) => query.placeholders))]);
	const values = $derived(logAttributes(pastedLogLine.text, placeholders));
	const unfilled = $derived(placeholders.filter((placeholder) => !values.has(placeholder)));
</script>

<IslandBoundary name="diagnose queries">
	<p>Helpful info.</p>
	<ol class="query-list">
		{#each queries as query (query.label)}
			<li>
				<p class="query-label">{query.label}</p>
				<div class="query-sql">
					<div class="query-actions">
						<CopyButton label="Copy query" text={filledSql(query.sql, values)} />
					</div>
					<HighlightedSql sql={query.sql} {values} />
				</div>
			</li>
		{/each}
	</ol>
	{#if unfilled.length > 0}
		<ThreadAside label="NOTE" title="Fill in your own values">
			<p>
				Make sure to substitute
				{#each unfilled as placeholder (placeholder)}<code class="placeholder-name"
						>{placeholder}</code
					>{/each}
			</p>
		</ThreadAside>
	{/if}
</IslandBoundary>

<style src="./diagnose-queries.css"></style>
