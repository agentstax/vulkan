<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import CompatMatrix from './compat-matrix.svelte';
	import type { ScopeExport } from './types';

	// pasted output of tools/compatexport, which decides every verdict: the
	// empty registry the library ships today, and the fixture its tests assert
	// against. Storybook cannot run Go and TS never computes a verdict, so
	// these stay literals
	const preRelease: ScopeExport = {
		version: 1,
		steps: [],
		rows: [
			{
				version: 1,
				min_compatible_version: 0,
				cells: [{ build_version: 1, support: 'supported' }],
			},
		],
	};

	const withTrail: ScopeExport = {
		version: 5,
		steps: [
			{ version: 2, min_compatible_version: 0 },
			{ version: 3, min_compatible_version: 0 },
			{ version: 4, min_compatible_version: 4 },
			{ version: 5, min_compatible_version: 0 },
		],
		rows: [
			{
				version: 1,
				min_compatible_version: 0,
				cells: [
					{ build_version: 1, support: 'supported' },
					{ build_version: 2, support: 'older_than_build' },
					{ build_version: 3, support: 'older_than_build' },
					{ build_version: 4, support: 'older_than_build' },
					{ build_version: 5, support: 'older_than_build' },
				],
			},
			{
				version: 2,
				min_compatible_version: 0,
				cells: [
					{ build_version: 1, support: 'supported' },
					{ build_version: 2, support: 'supported' },
					{ build_version: 3, support: 'older_than_build' },
					{ build_version: 4, support: 'older_than_build' },
					{ build_version: 5, support: 'older_than_build' },
				],
			},
			{
				version: 3,
				min_compatible_version: 0,
				cells: [
					{ build_version: 1, support: 'supported' },
					{ build_version: 2, support: 'supported' },
					{ build_version: 3, support: 'supported' },
					{ build_version: 4, support: 'older_than_build' },
					{ build_version: 5, support: 'older_than_build' },
				],
			},
			{
				version: 4,
				min_compatible_version: 4,
				cells: [
					{ build_version: 1, support: 'newer_than_build' },
					{ build_version: 2, support: 'newer_than_build' },
					{ build_version: 3, support: 'newer_than_build' },
					{ build_version: 4, support: 'supported' },
					{ build_version: 5, support: 'older_than_build' },
				],
			},
			{
				version: 5,
				min_compatible_version: 4,
				cells: [
					{ build_version: 1, support: 'newer_than_build' },
					{ build_version: 2, support: 'newer_than_build' },
					{ build_version: 3, support: 'newer_than_build' },
					{ build_version: 4, support: 'supported' },
					{ build_version: 5, support: 'supported' },
				],
			},
		],
	};

	const { Story } = defineMeta({
		title: 'Board/CompatMatrix',
		component: CompatMatrix,
		args: { label: 'Topic scope' },
	});
</script>

<!-- what the site renders today: no steps, so one pair to ask about -->
<Story name="Pre-release" args={{ scope: preRelease }} />

<!-- the window three wide at v3, closed to one column by the breaking step at
     v4, reopened from the new floor at v5 -->
<Story name="With a migration trail" args={{ scope: withTrail }} />
