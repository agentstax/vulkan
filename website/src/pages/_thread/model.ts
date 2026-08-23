import type { ThreadLink } from '../../components/prev-next/types';
import type { Board } from '../_board/boards';

export type ThreadData = {
	board: Board;
	postedDate: string;
	postCount: number;
	editHref: string;
	reportHref: string;
	previous: ThreadLink | null;
	next: ThreadLink | null;
};

export type CodeThreadData = {
	code: string;
	kind: 'error' | 'event' | 'metric';
	solved: boolean;
	classification: string;
	rank: string;
	introduction: string;
	logLine: string;
	consequence: string;
	fix: string | null;
};
