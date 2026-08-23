import type { CollectionEntry } from 'astro:content';
import { threadCode } from '../_board/boards';
import type { CodeThreadData } from './model';

type DocsEntry = CollectionEntry<'docs'>;

export function codeThreadData(entry: DocsEntry): CodeThreadData {
	const code = threadCode(entry.id);
	const { kind, recovery, level, consequence, fix } = entry.data;
	if (kind === undefined) {
		throw new Error(`entry "${entry.id}" carries no kind`);
	}
	if (consequence === undefined) {
		throw new Error(`entry "${entry.id}" carries no consequence`);
	}

	const title = entry.data.title;
	switch (kind) {
		case 'error': {
			if (recovery === undefined) {
				throw new Error(`error entry "${entry.id}" carries no recovery`);
			}
			return {
				code,
				kind,
				solved: fix !== undefined,
				classification: `recovery ${recovery}`,
				rank: recovery === 'permanent' ? 'Permanent error' : 'Transient error',
				introduction: "As it arrives in your log or error chain — the values are your call's own:",
				logLine: `${title}${fix === undefined ? '' : ` -- ${fix}`} [${code}]`,
				consequence,
				fix: fix ?? null,
			};
		}
		case 'event': {
			if (level === undefined) {
				throw new Error(`event entry "${entry.id}" carries no level`);
			}
			return {
				code,
				kind,
				solved: false,
				classification: `log event at ${level}`,
				rank: `Log event at ${level}`,
				introduction: 'As it arrives in your log:',
				logLine: `level=${level.toUpperCase()} msg="${title}" code=${code}`,
				consequence,
				fix: null,
			};
		}
		case 'metric': {
			return {
				code,
				kind,
				solved: false,
				classification: 'metric',
				rank: 'Metric',
				introduction: 'As it appears in your metrics:',
				logLine: title,
				consequence,
				fix: null,
			};
		}
	}
}
