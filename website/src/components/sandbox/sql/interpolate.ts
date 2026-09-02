// mirrors the fmt.Sprintf calls that assemble these statements in Go: verb [1]
// is the schema on every vulkan statement and the table names follow as [2],
// [3]. The sandbox is one installation in PGlite's own schema, so [1] is filled
// here rather than passed by every call site; values fill [2] onward, in order.
const sandboxSchema = 'public';

export function interpolate(template: string, ...values: (string | number)[]): string {
	return template.replace(/%\[(\d+)\]([sd])/g, (verb, position: string) => {
		const index = Number(position);
		if (index === 1) {
			return sandboxSchema;
		}
		const value = values[index - 2];
		if (value === undefined) {
			throw new Error(`SQL template wants ${verb} but only ${values.length} values were passed`);
		}
		return String(value);
	});
}
