<script lang="ts">
	import type { VersionManifest } from './types';

	type Props = {
		// the version this build carries, baked at build time
		version: string;
		// the build's own manifest until the live site's copy is fetched
		manifest: VersionManifest;
		// path of the page being read, carried into the chosen version's site
		path: string;
	};

	let { version, manifest, path }: Props = $props();

	const latestEntry = $derived(
		manifest.versions.find((entry) => entry.version === manifest.latest) ?? null,
	);
	const readingOldVersion = $derived(manifest.latest !== version);
	const versionListed = $derived(manifest.versions.some((entry) => entry.version === version));

	function switchVersion(event: Event): void {
		const chosen = (event.currentTarget as HTMLSelectElement).value;
		if (chosen === version) {
			return;
		}

		const entry = manifest.versions.find((row) => row.version === chosen);
		if (entry === undefined) {
			return;
		}
		window.location.assign(new URL(path, entry.url).href);
	}
</script>

<div class="version-select">
	<label>
		Version:
		<select value={version} onchange={switchVersion}>
			{#each manifest.versions as entry (entry.version)}
				<option value={entry.version}>
					{entry.version === manifest.latest ? `${entry.version} (latest)` : entry.version}
				</option>
			{/each}
			{#if !versionListed}
				<option value={version}>{version}</option>
			{/if}
		</select>
	</label>
	{#if readingOldVersion && latestEntry !== null}
		<span class="old-notice" role="status">
			You are reading the {version} docs —
			<a href={new URL(path, latestEntry.url).href}>go to the latest version ({manifest.latest})</a>
		</span>
	{/if}
</div>

<style src="./version-select.css"></style>
