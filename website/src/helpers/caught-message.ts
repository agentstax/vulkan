// What a caught value can show a reader. String() is never the fallback: a
// thrown object renders as "[object Object]", which names nothing.
export function caughtMessage(caught: unknown): string {
	if (caught instanceof Error && caught.message !== '') {
		return caught.message;
	}
	if (typeof caught === 'string' && caught !== '') {
		return caught;
	}
	return 'we done goofed it up — reload the page to try again';
}
