export type ExcerptSegment = {
	text: string;
	marked: boolean;
};

// a Pagefind excerpt arrives as HTML: matched words wrapped in <mark>,
// everything else entity-escaped; segments render as plain text so no
// markup from the index ever reaches the page
export function excerptSegments(excerpt: string): ExcerptSegment[] {
	const segments: ExcerptSegment[] = [];

	const pieces = excerpt.split('<mark>');
	pushSegment(segments, pieces[0] ?? '', false);
	for (const piece of pieces.slice(1)) {
		const closeAt = piece.indexOf('</mark>');
		if (closeAt === -1) {
			pushSegment(segments, piece, true);
			continue;
		}
		pushSegment(segments, piece.slice(0, closeAt), true);
		pushSegment(segments, piece.slice(closeAt + '</mark>'.length), false);
	}

	return segments;
}

// ***************
// *** HELPERS ***
// ***************

function pushSegment(segments: ExcerptSegment[], html: string, marked: boolean): void {
	if (html === '') {
		return;
	}
	segments.push({ text: decodeEntities(html), marked });
}

function decodeEntities(html: string): string {
	return html
		.replaceAll('&lt;', '<')
		.replaceAll('&gt;', '>')
		.replaceAll('&quot;', '"')
		.replaceAll('&#39;', "'")
		.replaceAll('&amp;', '&');
}
