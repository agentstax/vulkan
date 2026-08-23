import type { CollectionEntry } from 'astro:content';
import { lastCommitDate } from '../../helpers/last-commit-date';
import { repositoryUrl } from '../../site';
import type { ThreadLink } from '../../components/prev-next/types';
import { boards } from '../_board/boards';
import { boardEntries, entryFilePath, threadTitle } from '../_board/rows';
import type { ThreadData } from './model';

type DocsEntry = CollectionEntry<'docs'>;

export function threadData(entry: DocsEntry, docs: DocsEntry[]): ThreadData {
	const ids = docs.map((candidate) => candidate.id);
	const board = boards.find((candidate) => candidate.threads(ids).includes(entry.id));
	if (board === undefined) {
		throw new Error(`thread "${entry.id}" belongs to no board`);
	}

	const members = boardEntries(board, docs);
	const position = members.findIndex((member) => member.id === entry.id);
	const previousEntry = position > 0 ? members[position - 1] : undefined;
	const nextEntry = position < members.length - 1 ? members[position + 1] : undefined;

	return {
		board,
		postedDate: lastCommitDate(entryFilePath(entry)),
		postCount: docs.length,
		editHref: `${repositoryUrl}/edit/main/website/${entryFilePath(entry)}`,
		reportHref: `${repositoryUrl}/issues/new?title=${encodeURIComponent(`docs: ${entry.data.title}`)}`,
		previous: toThreadLink(previousEntry),
		next: toThreadLink(nextEntry),
	};
}

// ***************
// *** HELPERS ***
// ***************

function toThreadLink(entry: DocsEntry | undefined): ThreadLink | null {
	if (entry === undefined) {
		return null;
	}
	return { title: threadTitle(entry), href: `/${entry.id}/` };
}
