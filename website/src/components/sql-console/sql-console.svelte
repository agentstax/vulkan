<script lang="ts">
	import { sqlSegments } from './highlight';

	type Props = {
		label: string;
		sql: string;
		columns: string[];
		rows: (string | null)[][];
	};

	let { label, sql, columns, rows }: Props = $props();

	const segments = $derived(sqlSegments(sql));
</script>

<div class="sql-console">
	<div class="title-bar">
		<span class="console-label">{label}</span>
		<span class="console-meta">postgres 18 · wasm · local to this tab</span>
		<!-- inert until the PGlite island lands -->
		<button type="button" class="run-button">Run ▸</button>
	</div>
	<pre class="sql">{#each segments as segment, index (index)}{#if segment.keyword}<span
				class="sql-keyword">{segment.text}</span>{:else}{segment.text}{/if}{/each}</pre>
	<table class="result">
		<thead>
			<tr>
				{#each columns as column (column)}
					<th>{column}</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#each rows as row, index (index)}
				<tr>
					{#each row as cell, cellIndex (cellIndex)}
						<td>
							{#if cell === null}
								<span class="null-value">NULL</span>
							{:else}
								{cell}
							{/if}
						</td>
					{/each}
				</tr>
			{/each}
		</tbody>
	</table>
	<div class="status-bar">
		{rows.length}
		{rows.length === 1 ? 'row' : 'rows'}
	</div>
</div>

<style src="./sql-console.css"></style>
