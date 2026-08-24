import type { CollectionEntry } from 'astro:content';
import { lastCommitDate } from '../../helpers/last-commit-date';
import type { BoardRowData, StickyRowData, ThreadRowData } from './model';
import { boards, boardHref, stickyIds, isErrorThread, threadCode, type Board } from './boards';

type DocsEntry = CollectionEntry<'docs'>;

export function boardRows(docs: DocsEntry[]): BoardRowData[] {
	return boards.map((board) => {
		const entries = boardEntries(board, docs);

		const dated = entries.map((entry) => ({ entry, date: lastCommitDate(entryFilePath(entry)) }));
		dated.sort((a, b) => b.date.localeCompare(a.date));
		const newest = dated[0];
		if (newest === undefined) {
			throw new Error(`board "${board.title}" lists no threads`);
		}

		return {
			title: board.title,
			href: boardHref(board),
			description: board.description,
			threadCount: entries.length,
			lastPostTitle: threadTitle(newest.entry),
			lastPostHref: `/${newest.entry.id}/`,
			lastPostDate: newest.date,
			// visiting any of these pages counts as visiting the board
			scopeHrefs: [boardHref(board), ...entries.map((entry) => `/${entry.id}/`)],
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

export function threadRows(board: Board, docs: DocsEntry[]): ThreadRowData[] {
	return boardEntries(board, docs).map((entry) => ({
		title: threadTitle(entry),
		href: `/${entry.id}/`,
		lastUpdatedDate: lastCommitDate(entryFilePath(entry)),
	}));
}

// every thread on the board, newest change first -- the /whats-new/ page
// filters this against the visitor's own visit log at hydration
export function whatsNewRows(docs: DocsEntry[]): ThreadRowData[] {
	const rows = boards.flatMap((board) => threadRows(board, docs));
	rows.sort(
		(a, b) => b.lastUpdatedDate.localeCompare(a.lastUpdatedDate) || a.title.localeCompare(b.title),
	);
	return rows;
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

// an error thread's display title carries its code, everywhere the board
// names it
export function threadTitle(entry: DocsEntry): string {
	if (isErrorThread(entry.id)) {
		return `${entry.data.title} [${threadCode(entry.id)}]`;
	}
	return entry.data.title;
}

export function entryFilePath(entry: DocsEntry): string {
	if (entry.filePath === undefined) {
		throw new Error(`docs entry "${entry.id}" carries no filePath`);
	}
	return entry.filePath;
}
