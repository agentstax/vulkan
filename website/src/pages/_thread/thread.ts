import type { CollectionEntry } from 'astro:content';
import { lastCommitDate } from '../../helpers/last-commit-date';
import { repositoryUrl } from '../../site';
import { boards } from '../_board/boards';
import type { ThreadData } from './model';

type DocsEntry = CollectionEntry<'docs'>;

export function threadData(entry: DocsEntry, docs: DocsEntry[]): ThreadData {
	const board = boards.find((candidate) => candidate.contains(entry.id));
	if (board === undefined) {
		throw new Error(`thread "${entry.id}" belongs to no board`);
	}
	if (entry.filePath === undefined) {
		throw new Error(`docs entry "${entry.id}" carries no filePath`);
	}

	return {
		board,
		postedDate: lastCommitDate(entry.filePath),
		postCount: docs.length,
		editHref: `${repositoryUrl}/edit/main/website/${entry.filePath}`,
		reportHref: `${repositoryUrl}/issues/new?title=${encodeURIComponent(`docs: ${entry.data.title}`)}`,
	};
}
