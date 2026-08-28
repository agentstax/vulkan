<script lang="ts">
	import HighlightedSql from '../../../components/highlighted-sql/highlighted-sql.svelte';
	import type { PanelShell } from '../../../components/sandbox/types';

	type Props = {
		panel: PanelShell;
	};

	let { panel }: Props = $props();

	const emptyValues = new Map<string, string>();

	// the shell rows are the query's real build-time output, so an empty set
	// means the seed broke -- fail the build rather than render a bare table
	const exampleRow = panel.rows[0];
	if (exampleRow === undefined) {
		throw new Error('the example query returned no rows');
	}
</script>

<div class="query">
	<HighlightedSql sql={panel.sql} values={emptyValues} />
	<div class="example-scroll">
		<table class="example">
			<thead>
				<tr>
					{#each panel.columns as column, index (index)}
						<th>{column}</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				<tr>
					{#each exampleRow as cell, index (index)}
						<td>
							{#if cell === null}
								<span class="null-value">NULL</span>
							{:else}
								{cell}
							{/if}
						</td>
					{/each}
				</tr>
			</tbody>
		</table>
	</div>
</div>

<style src="./example-query.css"></style>
