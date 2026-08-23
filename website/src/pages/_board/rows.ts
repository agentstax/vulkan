import type { CollectionEntry } from 'astro:content';
import { lastCommitDate } from '../../helpers/last-commit-date';
import type { BoardRowData, StickyRowData } from './model';
import { boards, stickyIds, type Board } from './boards';

type DocsEntry = CollectionEntry<'docs'>;

export function boardRows(docs: DocsEntry[]): BoardRowData[] {
	return boards.map((board) => {
		const entries = boardEntries(board, docs);

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
			// visiting any of these pages counts as visiting the board
			scopeHrefs: [...new Set([board.href, ...entries.map((entry) => `/${entry.id}/`)])],
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

export function boardEntries(board: Board, docs: DocsEntry[]): DocsEntry[] {
	const ids = docs.map((entry) => entry.id);

	return board.threads(ids).map((id) => {
		const entry = docs.find((candidate) => candidate.id === id);
		if (entry === undefined) {
			throw new Error(`board "${board.title}" names thread "${id}" but no docs entry matches`);
		}
		return entry;
	});
}

export function entryFilePath(entry: DocsEntry): string {
	if (entry.filePath === undefined) {
		throw new Error(`docs entry "${entry.id}" carries no filePath`);
	}
	return entry.filePath;
}
