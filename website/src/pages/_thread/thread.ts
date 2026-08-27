import { lastCommitDate } from '../../helpers/last-commit-date';
import { repositoryUrl } from '../../site';
import type { ThreadLink } from '../../components/prev-next/types';
import { boards } from '../_board/boards';
import { boardThreads } from '../_board/rows';
import { repositoryFilePath, type Thread } from '../_board/threads';
import type { ThreadData } from './model';

export function threadData(thread: Thread, threads: Thread[]): ThreadData {
	const ids = threads.map((candidate) => candidate.id);
	const board = boards.find((candidate) => candidate.threads(ids).includes(thread.id));
	if (board === undefined) {
		throw new Error(`thread "${thread.id}" belongs to no board`);
	}

	const members = boardThreads(board, threads);
	const position = members.findIndex((member) => member.id === thread.id);
	const previousThread = position > 0 ? members[position - 1] : undefined;
	const nextThread = position < members.length - 1 ? members[position + 1] : undefined;

	return {
		board,
		postedDate: lastCommitDate(thread.filePath),
		postCount: threads.length,
		editHref: `${repositoryUrl}/edit/main/${repositoryFilePath(thread)}`,
		reportHref: `${repositoryUrl}/issues/new?title=${encodeURIComponent(`docs: ${thread.title}`)}`,
		previous: toThreadLink(previousThread),
		next: toThreadLink(nextThread),
	};
}

// ***************
// *** HELPERS ***
// ***************

function toThreadLink(thread: Thread | undefined): ThreadLink | null {
	if (thread === undefined) {
		return null;
	}
	return { title: thread.title, href: `/${thread.id}/` };
}
