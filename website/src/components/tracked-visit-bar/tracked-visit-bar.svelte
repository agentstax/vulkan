<script lang="ts">
	import { onMount } from 'svelte';
	import VisitBar from '../visit-bar/visit-bar.svelte';
	import type { VersionManifest } from '../version-select/types';
	import { readTracking } from '../../state/read-tracking.svelte';
	import { lastVisitDate, VersionManifestState } from './tracked-visit-bar-state.svelte';

	type Props = {
		version: string;
		// the manifest this build carries; the live site's copy replaces it
		// once fetched
		buildManifest: VersionManifest;
		manifestUrl: string;
	};

	let { version, buildManifest, manifestUrl }: Props = $props();

	const visitDate = lastVisitDate();

	const manifestState = new VersionManifestState(buildManifest);
	let path = $state('');

	// the island carries transition:persist, so this instance lives for the
	// whole visit: onMount runs once, and every later navigation reaches the
	// bar through astro:after-swap instead
	onMount(() => {
		const recordVisit = () => {
			readTracking.recordPageVisit(window.location.pathname);
			path = window.location.pathname;
		};

		recordVisit();
		void manifestState.load(manifestUrl);

		document.addEventListener('astro:after-swap', recordVisit);
		return () => document.removeEventListener('astro:after-swap', recordVisit);
	});
</script>

<VisitBar
	lastVisitDate={visitDate === null ? null : visitDate.slice(0, 10)}
	{version}
	manifest={manifestState.manifest}
	{path}
/>
