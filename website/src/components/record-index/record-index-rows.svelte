<script lang="ts">
	import type { RecordIndexRow } from './types';

	type Props = {
		rows: RecordIndexRow[];
	};

	let { rows }: Props = $props();
</script>

<table>
	<thead>
		<tr>
			<th>Record</th>
			<th>Date</th>
			<th>Decision</th>
			<th>Status</th>
		</tr>
	</thead>
	<tbody>
		{#each rows as row (row.number)}
			<tr data-status={row.status}>
				<td><a href={row.href}>{row.number}</a></td>
				<td class="date">{row.date}</td>
				<td>{row.title}</td>
				<td>{row.status}</td>
			</tr>
		{/each}
	</tbody>
</table>

<style src="./record-index-rows.css"></style>
