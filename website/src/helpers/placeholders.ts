// Reading {attribute} values out of a log line a reader pasted, and filling a
// declared fix with them. The names are the log attribute registry's.

export type FilledSegmentKind = 'plain' | 'placeholder' | 'value';

export type FilledSegment = {
	text: string;
	kind: FilledSegmentKind;
};

const placeholderPattern = /\{([a-z][a-z0-9_]*)\}/g;
const attributeName = /^[a-z][a-z0-9_]*$/;

// Nothing parses the line's grammar -- each name is looked up on its own.
export function logAttributes(line: string, names: string[]): Map<string, string> {
	const found = new Map<string, string>();

	for (const name of names) {
		const value = attributeValue(line, name);
		if (value !== null) {
			found.set(name, value);
		}
	}
	return found;
}

// A name nothing filled stays a visible blank.
export function fillText(text: string, values: Map<string, string>): FilledSegment[] {
	const segments: FilledSegment[] = [];

	let cursor = 0;
	for (const match of text.matchAll(placeholderPattern)) {
		if (match.index > cursor) {
			segments.push({ text: text.slice(cursor, match.index), kind: 'plain' });
		}
		const value = match[1] === undefined ? undefined : values.get(match[1]);
		segments.push(
			value === undefined
				? { text: match[0], kind: 'placeholder' }
				: { text: value, kind: 'value' },
		);
		cursor = match.index + match[0].length;
	}

	if (cursor < text.length) {
		segments.push({ text: text.slice(cursor), kind: 'plain' });
	}
	return segments;
}

// ***************
// *** HELPERS ***
// ***************

// Three shapes carry a value, tried in order:
//   text handler / CLI block   topic=orders  topic="orders"
//   JSON                       "topic": "orders"
//   the Error() one-liner      topic "orders", version 3
//
// The one-liner pattern is the strict one: a problem line repeats attribute
// names, so `topic not found` must not read "not" as the topic. Only a quoted
// string or a digit-leading value counts, which is what the one-liner renders.
function attributeValue(line: string, name: string): string | null {
	if (!attributeName.test(name)) {
		return null;
	}

	const patterns = [
		new RegExp(`\\b${name}=(?:"([^"]*)"|(\\S+))`),
		new RegExp(`"${name}"\\s*:\\s*(?:"([^"]*)"|([^,}\\s]+))`),
		new RegExp(`\\b${name}\\s+(?:"([^"]*)"|(\\d[^\\s,]*))`),
	];
	for (const pattern of patterns) {
		const match = pattern.exec(line);
		const value = match?.[1] ?? match?.[2];
		if (value !== undefined) {
			// an unquoted value never legitimately ends on the separator
			return value.replace(/,$/, '');
		}
	}
	return null;
}
