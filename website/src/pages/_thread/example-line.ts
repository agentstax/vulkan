// Composes the example log line at the top of a code thread, with example
// attribute values so pasting it into the paste box fills the thread's
// queries and fix. One value per registry attribute name, shared by every
// thread — the same fictional deployment across the whole board.

import { fillText } from '../../helpers/placeholders';

// declaration order is the order values render on a composed line
export const exampleValues: Record<string, string> = {
	schema: 'vulkan',
	topic: 'orders.created',
	topic_id: '1',
	group: 'charge-cards',
	group_id: '7',
	message_id: '214',
	message_key: 'order-214',
	low: '4100',
	high: '4200',
	version: '2',
	build_version: '3',
	owner_kind: 'topic',
	schedule: 'alert.partition_count',
	schedule_id: '3',
	worker: 'topic_janitor',
	worker_id: '12',
	existing_partition_size: '1000000',
};

// VK0023 is VK0022's mirror -- there the database is ahead of the build, so
// the shared ordering would render a fix that migrates downward
export const codeOverrides: Record<string, Record<string, string>> = {
	VK0023: { version: '3', build_version: '2' },
};

// the Error() one-liner: problem: name value, name value -- fix [code]
export function errorExampleLine(
	problem: string,
	fix: string | null,
	code: string,
	names: string[],
): string {
	const values = orderedValues(code, names);
	const pairs = values.map(([name, value]) => `${name} ${oneLinerValue(value)}`).join(', ');
	const filledFix = fix === null ? '' : ` -- ${fillRaw(fix, values)}`;
	return `${problem}${pairs === '' ? '' : `: ${pairs}`}${filledFix} [${code}]`;
}

// the text-handler line: level=WARN msg="..." code=VK0026 name=value
export function eventExampleLine(
	message: string,
	level: string,
	code: string,
	names: string[],
): string {
	const attributes = orderedValues(code, names)
		.map(([name, value]) => ` ${name}=${value}`)
		.join('');
	return `level=${level.toUpperCase()} msg="${message}" code=${code}${attributes}`;
}

// ***************
// *** HELPERS ***
// ***************

function orderedValues(code: string, names: string[]): [string, string][] {
	const missing = names.filter((name) => exampleValues[name] === undefined);
	if (missing.length > 0) {
		throw new Error(`code ${code} names attributes with no example value: ${missing.join(', ')}`);
	}

	const overrides = codeOverrides[code] ?? {};
	const values: [string, string][] = [];
	for (const [name, value] of Object.entries(exampleValues)) {
		if (!names.includes(name)) {
			continue;
		}
		values.push([name, overrides[name] ?? value]);
	}
	return values;
}

// the paste box's one-liner pattern reads only a quoted string or a
// digit-leading value, which is what the library's own renderer emits
function oneLinerValue(value: string): string {
	return /^\d/.test(value) ? value : `"${value}"`;
}

function fillRaw(text: string, values: [string, string][]): string {
	return fillText(text, new Map(values))
		.map((segment) => segment.text)
		.join('');
}
