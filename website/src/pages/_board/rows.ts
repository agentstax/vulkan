import { lastCommitDate } from '../../helpers/last-commit-date';
import type { BoardRowData, StickyRowData, ThreadRowData } from './model';
import { boards, boardHref, stickyIds, type Board } from './boards';
import type { Thread } from './threads';

export function boardRows(threads: Thread[]): BoardRowData[] {
	return boards.map((board) => {
		const members = boardThreads(board, threads);

		const dated = members.map((thread) => ({ thread, date: lastCommitDate(thread.filePath) }));
		dated.sort((a, b) => b.date.localeCompare(a.date));
		const newest = dated[0];
		if (newest === undefined) {
			throw new Error(`board "${board.title}" lists no threads`);
		}

		return {
			title: board.title,
			href: boardHref(board),
			description: board.description,
			threadCount: members.length,
			lastPostTitle: newest.thread.title,
			lastPostHref: `/${newest.thread.id}/`,
			lastPostDate: newest.date,
			// visiting any of these pages counts as visiting the board
			scopeHrefs: [boardHref(board), ...members.map((thread) => `/${thread.id}/`)],
		};
	});
}

export function stickyRows(threads: Thread[]): StickyRowData[] {
	return stickyIds.map((id) => {
		const thread = threads.find((candidate) => candidate.id === id);
		if (thread === undefined) {
			throw new Error(`sticky "${id}" matches no thread`);
		}

		return {
			title: thread.title,
			href: `/${id}/`,
			lastUpdatedDate: lastCommitDate(thread.filePath),
		};
	});
}

export function threadRows(board: Board, threads: Thread[]): ThreadRowData[] {
	return boardThreads(board, threads).map((thread) => ({
		title: thread.title,
		href: `/${thread.id}/`,
		lastUpdatedDate: lastCommitDate(thread.filePath),
	}));
}

// every thread on the board, newest change first -- the /whats-new/ page
// filters this against the visitor's own visit log at hydration
export function whatsNewRows(threads: Thread[]): ThreadRowData[] {
	const rows = boards.flatMap((board) => threadRows(board, threads));
	rows.sort(
		(a, b) => b.lastUpdatedDate.localeCompare(a.lastUpdatedDate) || a.title.localeCompare(b.title),
	);
	return rows;
}

export function boardThreads(board: Board, threads: Thread[]): Thread[] {
	const ids = threads.map((thread) => thread.id);

	return board.threads(ids).map((id) => {
		const thread = threads.find((candidate) => candidate.id === id);
		if (thread === undefined) {
			throw new Error(`board "${board.title}" names thread "${id}" but no thread matches`);
		}
		return thread;
	});
}
