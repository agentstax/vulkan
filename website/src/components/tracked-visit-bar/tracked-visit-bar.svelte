<script lang="ts">
	import { onMount } from 'svelte';
	import VisitBar from '../visit-bar/visit-bar.svelte';
	import { readTracking } from '../../state/read-tracking.svelte';
	import { VersionManifestState } from './tracked-visit-bar-state.svelte';

	type Props = {
		version: string;
		manifestUrl: string;
	};

	let { version, manifestUrl }: Props = $props();

	// captured before this page view is appended, so the bar shows the
	// PREVIOUS visit
	const lastVisitDate = readTracking.lastVisitDate();

	const manifestState = new VersionManifestState();
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
	manifestFetched={manifestState.phase === 'done'}
	{path}
/>
