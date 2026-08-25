<script lang="ts">
	import type { RunResult } from '../sandbox/database';

	type Props = {
		result: RunResult;
	};

	let { result }: Props = $props();

	// no id column to key on -- a panel runs whatever SQL the reader typed -- so
	// a row is identified by its cells, and the repeat count keeps duplicates apart
	const rowKeys: string[] = $derived.by(() => {
		const repeats: Record<string, number> = {};
		return result.rows.map((cells) => {
			const content = JSON.stringify(cells);
			repeats[content] = (repeats[content] ?? 0) + 1;
			return `${content}#${repeats[content]}`;
		});
	});

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
				{#each result.rows as cells, index (rowKeys[index])}
					<tr>
						{#each cells as cell, cellIndex (cellIndex)}
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
