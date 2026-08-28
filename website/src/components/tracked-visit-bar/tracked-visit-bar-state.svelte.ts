import type { VersionEntry, VersionManifest } from '../version-select/types';

class VersionManifestState {
	manifest: VersionManifest;
	private loadStarted = false;

	constructor(buildManifest: VersionManifest) {
		this.manifest = $state(buildManifest);
	}

	// one fetch per visit: ClientRouter swaps re-mount the island but keep
	// the module graph, so repeat calls after the first are no-ops
	async load(manifestUrl: string): Promise<void> {
		if (this.loadStarted) {
			return;
		}
		this.loadStarted = true;

		try {
			const response = await fetch(manifestUrl);
			if (!response.ok) {
				return;
			}

			const fetched = toManifest((await response.json()) as unknown);
			if (fetched !== null) {
				this.manifest = fetched;
			}
		} catch {
			// unreachable live site -- the build's own manifest stays
		}
	}
}

// the visit's one instance: each navigation mounts a fresh island, and
// every mount after the first must render the already-fetched manifest
// on its first paint instead of starting over from the build's copy
let visitInstance: VersionManifestState | null = null;

export function versionManifestState(buildManifest: VersionManifest): VersionManifestState {
	if (visitInstance === null) {
		visitInstance = new VersionManifestState(buildManifest);
	}
	return visitInstance;
}

// ***************
// *** HELPERS ***
// ***************

function toManifest(value: unknown): VersionManifest | null {
	if (typeof value !== 'object' || value === null) {
		return null;
	}

	const record = value as Record<string, unknown>;
	if (typeof record.latest !== 'string' || !Array.isArray(record.versions)) {
		return null;
	}

	const versions: VersionEntry[] = [];
	for (const item of record.versions as unknown[]) {
		if (typeof item !== 'object' || item === null) {
			return null;
		}
		const row = item as Record<string, unknown>;
		if (typeof row.version !== 'string' || typeof row.url !== 'string') {
			return null;
		}
		versions.push({ version: row.version, url: row.url });
	}

	return { latest: record.latest, versions };
}
