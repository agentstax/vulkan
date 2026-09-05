import type { CollectionEntry } from 'astro:content';
import { threadCode } from '../_board/boards';
import { pastePlaceholders } from './diagnose';
import { errorExampleLine, eventExampleLine } from './example-line';
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
	const names = pastePlaceholders(code);
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
				introduction:
					names.length > 0
						? 'As it arrives in your log or error chain — example values stand where yours will:'
						: "As it arrives in your log or error chain — the values are your call's own:",
				logLine: errorExampleLine(title, fix ?? null, code, names),
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
				introduction:
					names.length > 0
						? 'As it arrives in your log — example values stand where yours will:'
						: 'As it arrives in your log:',
				logLine: eventExampleLine(title, level, code, names),
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
		case 'alert': {
			return {
				code,
				kind,
				solved: false,
				classification: 'alert',
				rank: 'Alert',
				introduction: 'As it appears on __system.alerts:',
				logLine: title,
				consequence,
				fix: null,
			};
		}
	}
}
