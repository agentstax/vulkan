// mirrors the fmt.Sprintf calls that assemble these statements in Go:
// each %s or %d is replaced, in order, by the next value
export function interpolate(template: string, ...values: (string | number)[]): string {
	let result = template;
	for (const value of values) {
		result = result.replace(/%[sd]/, String(value));
	}
	return result;
}
