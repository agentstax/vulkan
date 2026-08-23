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
