import type { CollectionEntry } from 'astro:content';
import { lastCommitDate } from '../../helpers/last-commit-date';
import type { BoardRowData, StickyRowData } from './model';
import { boards, stickyIds } from './boards';

type DocsEntry = CollectionEntry<'docs'>;

export function boardRows(docs: DocsEntry[]): BoardRowData[] {
	return boards.map((board) => {
		const entries = docs.filter((entry) => board.contains(entry.id));
		if (entries.length === 0) {
			throw new Error(`board "${board.title}" matches no docs entries`);
		}

		const dated = entries.map((entry) => ({ entry, date: lastCommitDate(entryFilePath(entry)) }));
		dated.sort((a, b) => b.date.localeCompare(a.date));
		const newest = dated[0];

		return {
			title: board.title,
			href: board.href,
			description: board.description,
			threadCount: entries.length,
			lastPostTitle: newest.entry.data.title,
			lastPostHref: `/${newest.entry.id}/`,
			lastPostDate: newest.date,
		};
	});
}

export function stickyRows(docs: DocsEntry[]): StickyRowData[] {
	return stickyIds.map((id) => {
		const entry = docs.find((candidate) => candidate.id === id);
		if (entry === undefined) {
			throw new Error(`sticky "${id}" matches no docs entry`);
		}

		return {
			title: entry.data.title,
			href: `/${id}/`,
			lastUpdatedDate: lastCommitDate(entryFilePath(entry)),
		};
	});
}

function entryFilePath(entry: DocsEntry): string {
	if (entry.filePath === undefined) {
		throw new Error(`docs entry "${entry.id}" carries no filePath`);
	}
	return entry.filePath;
}
