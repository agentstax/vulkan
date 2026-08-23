import { readdirSync } from 'node:fs';
import type { CollectionEntry } from 'astro:content';
import { isErrorThread } from './boards';
import type { SiteStats } from './model';

type DocsEntry = CollectionEntry<'docs'>;

// the build runs from website/, so the decision records sit one level up
const decisionRecordsDirectory = '../docs/decisions';

export function siteStats(docs: DocsEntry[]): SiteStats {
	const decisionRecords = readdirSync(decisionRecordsDirectory).filter((name) =>
		name.endsWith('.md'),
	);

	return {
		threadCount: docs.length,
		codeCount: docs.filter((entry) => isErrorThread(entry.id)).length,
		decisionRecordCount: decisionRecords.length,
	};
}
