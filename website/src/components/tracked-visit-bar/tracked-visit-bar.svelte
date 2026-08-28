<script lang="ts">
	import { onMount } from 'svelte';
	import VisitBar from '../visit-bar/visit-bar.svelte';
	import type { VersionManifest } from '../version-select/types';
	import { readTracking } from '../../state/read-tracking.svelte';
	import { versionManifestState } from './tracked-visit-bar-state.svelte';

	type Props = {
		version: string;
		// the manifest this build carries; the live site's copy replaces it
		// once fetched
		buildManifest: VersionManifest;
		manifestUrl: string;
	};

	let { version, buildManifest, manifestUrl }: Props = $props();

	// captured before this page view is appended, so the bar shows the
	// PREVIOUS visit
	const lastVisitDate = readTracking.lastVisitDate();

	const manifestState = versionManifestState(buildManifest);
	let path = $state('');

	onMount(() => {
		readTracking.recordPageVisit(window.location.pathname);
		path = window.location.pathname;
		void manifestState.load(manifestUrl);
	});
</script>

<VisitBar
	lastVisitDate={lastVisitDate === null ? null : lastVisitDate.slice(0, 10)}
	{version}
	manifest={manifestState.manifest}
	{path}
/>
