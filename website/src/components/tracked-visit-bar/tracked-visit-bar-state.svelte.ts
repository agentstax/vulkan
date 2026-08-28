import type { VersionEntry, VersionManifest } from '../version-select/types';

export class VersionManifestState {
	manifest: VersionManifest;

	constructor(buildManifest: VersionManifest) {
		this.manifest = $state(buildManifest);
	}

	async load(manifestUrl: string): Promise<void> {
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
