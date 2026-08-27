import { isDecisionRecordThread, isErrorThread } from './boards';
import type { SiteStats } from './model';
import type { Thread } from './threads';

export function siteStats(threads: Thread[]): SiteStats {
	return {
		// the doc threads alone -- the records get their own line
		docCount: threads.filter((thread) => !isDecisionRecordThread(thread.id)).length,
		codeCount: threads.filter((thread) => isErrorThread(thread.id)).length,
		decisionRecordCount: threads.filter((thread) => isDecisionRecordThread(thread.id)).length,
	};
}
