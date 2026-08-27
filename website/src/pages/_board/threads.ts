import type { CollectionEntry } from 'astro:content';
import { recordTitle } from '../../helpers/decision-records';
import { isErrorThread, threadCode } from './boards';

export type DocsEntry = CollectionEntry<'docs'>;
export type DecisionEntry = CollectionEntry<'decisions'>;

// one shape for every page a board can name, whichever collection it
// comes from; the page's URL is /<id>/
export type Thread = {
	id: string;
	title: string;
	description: string;
	// path relative to website/, where the build runs
	filePath: string;
	entry: DocsEntry | DecisionEntry;
};

export function siteThreads(docs: DocsEntry[], decisions: DecisionEntry[]): Thread[] {
	return [...docs.map(docsThread), ...decisions.map(decisionThread)];
}

// the repo-rooted path GitHub links need; the records live outside website/
export function repositoryFilePath(thread: Thread): string {
	if (thread.entry.collection === 'decisions') {
		return `docs/decisions/${fileName(thread.filePath)}`;
	}
	return `website/${thread.filePath}`;
}

// ***************
// *** HELPERS ***
// ***************

function docsThread(entry: DocsEntry): Thread {
	// an error thread's display title carries its code, everywhere the
	// board names it
	const title = isErrorThread(entry.id)
		? `${entry.data.title} [${threadCode(entry.id)}]`
		: entry.data.title;

	return {
		id: entry.id,
		title,
		description: entry.data.description ?? entry.data.title,
		filePath: entryFilePath(entry),
		entry,
	};
}

function decisionThread(entry: DecisionEntry): Thread {
	const title = `${entry.id} — ${recordTitle(entry.id, entry.body)}`;

	return {
		id: `decisions/${entry.id}`,
		title,
		description: title,
		filePath: entryFilePath(entry),
		entry,
	};
}

function entryFilePath(entry: DocsEntry | DecisionEntry): string {
	if (entry.filePath === undefined) {
		throw new Error(`entry "${entry.id}" carries no filePath`);
	}
	return entry.filePath;
}

function fileName(filePath: string): string {
	const name = filePath.split('/').pop();
	if (name === undefined || name === '') {
		throw new Error(`path "${filePath}" carries no file name`);
	}
	return name;
}
