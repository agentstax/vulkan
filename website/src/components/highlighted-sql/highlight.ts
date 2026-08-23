export type SqlSegment = {
	text: string;
	keyword: boolean;
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

// sqlSegments feeds the static shell's highlighting; the CodeMirror editor
// replaces the whole shell once the live console mounts.
export function sqlSegments(sql: string): SqlSegment[] {
	const segments: SqlSegment[] = [];

	let cursor = 0;
	for (const match of sql.matchAll(keywordPattern)) {
		if (match.index > cursor) {
			segments.push({ text: sql.slice(cursor, match.index), keyword: false });
		}
		segments.push({ text: match[0], keyword: true });
		cursor = match.index + match[0].length;
	}

	if (cursor < sql.length) {
		segments.push({ text: sql.slice(cursor), keyword: false });
	}
	return segments;
}
