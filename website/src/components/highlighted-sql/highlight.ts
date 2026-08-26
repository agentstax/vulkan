export type SqlSegmentKind = 'plain' | 'keyword' | 'placeholder';

export type SqlSegment = {
	text: string;
	kind: SqlSegmentKind;
};

const keywords = [
	'SELECT',
	'FROM',
	'WHERE',
	'ORDER BY',
	'GROUP BY',
	'LIMIT',
	'DESC',
	'ASC',
	'INSERT',
	'INTO',
	'VALUES',
	'UPDATE',
	'SET',
	'DELETE',
	'WITH',
	'AS',
	'RETURNING',
	'JOIN',
	'ON',
	'AND',
	'OR',
	'NOT',
	'NULL',
];

const keywordPattern = new RegExp(`\\b(${keywords.join('|').replaceAll(' ', '\\s+')})\\b`, 'gi');

// a declared diagnose query names the values the reader substitutes as
// {attribute_name} -- the log attribute keys their own line already carries
const placeholderPattern = /\{[a-z][a-z0-9_]*\}/g;

// sqlSegments feeds the static shell's highlighting; the CodeMirror editor
// replaces the whole shell once the live console mounts.
export function sqlSegments(sql: string): SqlSegment[] {
	const segments: SqlSegment[] = [];

	let cursor = 0;
	for (const match of sql.matchAll(placeholderPattern)) {
		pushKeywordSegments(segments, sql.slice(cursor, match.index));
		segments.push({ text: match[0], kind: 'placeholder' });
		cursor = match.index + match[0].length;
	}
	pushKeywordSegments(segments, sql.slice(cursor));

	return segments;
}

// ***************
// *** HELPERS ***
// ***************

function pushKeywordSegments(segments: SqlSegment[], sql: string): void {
	if (sql === '') {
		return;
	}

	let cursor = 0;
	for (const match of sql.matchAll(keywordPattern)) {
		if (match.index > cursor) {
			segments.push({ text: sql.slice(cursor, match.index), kind: 'plain' });
		}
		segments.push({ text: match[0], kind: 'keyword' });
		cursor = match.index + match[0].length;
	}

	if (cursor < sql.length) {
		segments.push({ text: sql.slice(cursor), kind: 'plain' });
	}
}
