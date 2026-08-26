export type SqlSegmentKind = 'plain' | 'keyword' | 'placeholder' | 'value';

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

// The declared SQL already carries the quoting each position needs, and that
// is what decides how a value goes in: inside quotes a text literal, bare an
// identifier or a number.
export function fillSegments(segments: SqlSegment[], values: Map<string, string>): SqlSegment[] {
	return segments.map((segment, index) => {
		if (segment.kind !== 'placeholder') {
			return segment;
		}
		const value = values.get(segment.text.slice(1, -1));
		if (value === undefined) {
			return segment;
		}

		if (isQuotedPosition(segments, index)) {
			return { text: value.replaceAll("'", "''"), kind: 'value' };
		}
		// nothing else is a legal identifier or number, so rather than write
		// SQL that cannot run, the blank stays a blank
		if (!/^[A-Za-z0-9_]+$/.test(value)) {
			return segment;
		}
		return { text: value, kind: 'value' };
	});
}

// The copy button hands over exactly the text the block shows, so a query
// still holding a blank is copied with the blank visible in it.
export function filledSql(sql: string, values: Map<string, string>): string {
	return fillSegments(sqlSegments(sql), values)
		.map((segment) => segment.text)
		.join('');
}

// ***************
// *** HELPERS ***
// ***************

// A value position reads `'{topic}'`.
function isQuotedPosition(segments: SqlSegment[], index: number): boolean {
	const before = segments[index - 1];
	const after = segments[index + 1];
	return (
		before !== undefined &&
		after !== undefined &&
		before.text.endsWith("'") &&
		after.text.startsWith("'")
	);
}

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
