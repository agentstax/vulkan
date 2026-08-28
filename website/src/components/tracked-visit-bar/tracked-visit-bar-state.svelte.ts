import type { VersionEntry, VersionManifest } from '../version-select/types';

// loading: the manifest fetch has not finished
// done: the fetch finished -- manifest stays null when it could not be read
export type VersionManifestPhase = 'loading' | 'done';

export class VersionManifestState {
	phase: VersionManifestPhase = $state('loading');
	manifest: VersionManifest | null = $state(null);

	async load(manifestUrl: string): Promise<void> {
		try {
			const response = await fetch(manifestUrl);
			if (response.ok) {
				this.manifest = toManifest((await response.json()) as unknown);
			}
		} catch {
			// unreachable manifest -- the bar shows only this build's version
		}
		this.phase = 'done';
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
