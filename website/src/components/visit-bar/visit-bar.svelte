<script lang="ts">
	import VersionSelect from '../version-select/version-select.svelte';
	import type { VersionManifest } from '../version-select/types';

	type Props = {
		lastVisitDate: string | null;
		// the version this build carries, baked at build time
		version: string;
		// null until the live site's manifest is fetched, and when it could
		// not be read
		manifest: VersionManifest | null;
		// true once the manifest fetch finished, however it ended
		manifestFetched: boolean;
		// path of the page being read, carried into a chosen version's site
		path: string;
	};

	let { lastVisitDate, version, manifest, manifestFetched, path }: Props = $props();

	const readingOldVersion = $derived(manifest !== null && manifest.latest !== version);
</script>

<div class="visit-bar" data-old-version={readingOldVersion}>
	<div class="version-area">
		{#if manifestFetched}
			<VersionSelect {version} {manifest} {path} />
		{/if}
	</div>
	{#if lastVisitDate === null}
		<span>Welcome — this is your first visit</span>
	{:else}
		<span class="visit-area">
			<span class="visit-date">You last visited on {lastVisitDate}</span>
			<span aria-hidden="true">|</span>
			<a href="/whats-new/">Show what's new since then</a>
		</span>
	{/if}
</div>

<style src="./visit-bar.css"></style>
