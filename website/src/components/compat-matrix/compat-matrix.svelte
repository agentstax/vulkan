<script lang="ts">
	import type { ScopeExport, SchemaSupport } from './types';

	type Props = {
		scope: ScopeExport;
		label: string;
	};

	let { scope, label }: Props = $props();

	// what the reader has to do, not which side is wrong -- the legend's code
	// links carry the diagnosis
	const actions: Record<SchemaSupport, string> = {
		supported: 'runs',
		older_than_build: 'migrate',
		newer_than_build: 'upgrade',
	};

	const explanations: Record<SchemaSupport, string> = {
		supported: 'this build runs against this schema',
		older_than_build: 'VK0022 — migrate the database up first',
		newer_than_build: 'VK0023 — upgrade the binary',
	};

	const builds = $derived(scope.rows[0]?.cells ?? []);
	const steps = $derived(
		scope.steps.map((step) => ({
			version: step.version,
			breaking: step.min_compatible_version > 0,
		})),
	);
</script>

<figure class="compat-matrix">
	<figcaption class="matrix-label">{label} — v{scope.version}</figcaption>

	<div class="matrix-scroll">
		<table class="matrix">
			<thead>
				<tr>
					<td class="matrix-corner"></td>
					{#each builds as cell (cell.build_version)}
						<th class="matrix-head" scope="col">build v{cell.build_version}</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each scope.rows as row (row.version)}
					<tr>
						<th class="matrix-side" scope="row">database v{row.version}</th>
						{#each row.cells as cell (cell.build_version)}
							<td
								class="matrix-cell"
								data-support={cell.support}
								title={explanations[cell.support]}
							>
								{actions[cell.support]}
							</td>
						{/each}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>

	<ul class="matrix-legend">
		<li class="legend-item" data-support="supported">
			<span class="legend-swatch"></span>runs
		</li>
		<li class="legend-item" data-support="older_than_build">
			<span class="legend-swatch"></span>migrate the database up (<a href="/errors/VK0022">VK0022</a
			>)
		</li>
		<li class="legend-item" data-support="newer_than_build">
			<span class="legend-swatch"></span>upgrade the binary (<a href="/errors/VK0023">VK0023</a>)
		</li>
	</ul>

	{#if steps.length > 0}
		<p class="matrix-steps">
			{#each steps as step (step.version)}
				<span class="step" data-breaking={step.breaking}>
					v{step.version}
					{step.breaking ? 'breaking' : 'additive'}
				</span>
			{/each}
		</p>
	{/if}
</figure>

<style src="./compat-matrix.css"></style>
