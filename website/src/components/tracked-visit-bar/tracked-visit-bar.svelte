<script lang="ts">
	import { onMount } from 'svelte';
	import VisitBar from '../visit-bar/visit-bar.svelte';
	import { readTracking } from '../../state/read-tracking.svelte';

	// captured before this page view is appended, so the bar shows the
	// PREVIOUS visit
	const lastVisitDate = readTracking.lastVisitDate();

	onMount(() => {
		readTracking.recordPageVisit(window.location.pathname);
	});
</script>

<VisitBar lastVisitDate={lastVisitDate === null ? null : lastVisitDate.slice(0, 10)} />
