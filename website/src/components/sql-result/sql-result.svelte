<script lang="ts">
	import type { RunResult } from '../sql-console/database';

	type Props = {
		result: RunResult;
	};

	let { result }: Props = $props();

	const statusText = $derived.by(() => {
		const parts: string[] = [];
		if (result.affectedRows === null) {
			parts.push(`${result.rows.length} ${result.rows.length === 1 ? 'row' : 'rows'}`);
		} else {
			parts.push(`${result.affectedRows} ${result.affectedRows === 1 ? 'row' : 'rows'} affected`);
		}
		if (result.durationMs !== null) parts.push(`${result.durationMs.toFixed(1)} ms`);
		if (result.statementCount > 1) parts.push(`${result.statementCount} statements`);
		return parts.join(' · ');
	});
</script>

{#if result.columns.length > 0}
	<div class="result-scroll">
		<table class="result">
			<thead>
				<tr>
					{#each result.columns as column, index (index)}
						<th>{column}</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each result.rows as row, index (index)}
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
	</div>
{/if}
<div class="status-bar">{statusText}</div>

<style src="./sql-result.css"></style>
