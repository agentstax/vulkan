// one row of /versions.json -- a deployed docs version and the origin
// that serves it
export type VersionEntry = {
	version: string;
	url: string;
};

// the live site's registry of every deployed docs version; frozen
// deployments fetch it at read time, so their version list never goes stale
export type VersionManifest = {
	latest: string;
	versions: VersionEntry[];
};
