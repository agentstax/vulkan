import { describe, expect, it } from 'vitest';
import { codeRecords } from '../../data/codes';
import { logAttributes } from '../../helpers/placeholders';
import { codeOverrides, errorExampleLine, eventExampleLine, exampleValues } from './example-line';

// The self-demonstration invariant: every code's composed example line,
// pasted into the paste box, fills every placeholder the thread has.
describe('composed example lines round-trip through the paste parser', () => {
	for (const [code, record] of Object.entries(codeRecords)) {
		if (record.kind === 'metric' || record.kind === 'alert') {
			continue;
		}

		const names = [
			...new Set([
				...(record.queries ?? []).flatMap((query) => query.placeholders),
				...(record.kind === 'error' ? (record.fix_placeholders ?? []) : []),
			]),
		];
		if (names.length === 0) {
			continue;
		}

		it(`${code} fills all of: ${names.join(', ')}`, () => {
			const line =
				record.kind === 'error'
					? errorExampleLine(record.problem, record.fix ?? null, code, names)
					: eventExampleLine(record.message, 'warn', code, names);

			expect(line).not.toContain('{');

			const found = logAttributes(line, names);
			for (const name of names) {
				expect(found.get(name), `${name} in: ${line}`).toBe(
					codeOverrides[code]?.[name] ?? exampleValues[name],
				);
			}
		});
	}
});

describe('the composed shapes', () => {
	it('renders the Error() one-liner with pairs and a filled fix', () => {
		const line = errorExampleLine(
			'schema version is older than this build requires',
			'migrate the {owner_kind} schema up from {version} to {build_version}',
			'VK0022',
			['owner_kind', 'version', 'build_version'],
		);

		expect(line).toBe(
			'schema version is older than this build requires: version 2, build_version 3, owner_kind "topic" -- migrate the topic schema up from 2 to 3 [VK0022]',
		);
	});

	it('renders the text-handler line with bare values', () => {
		const line = eventExampleLine('lease reclaimed from expired worker', 'warn', 'VK0026', [
			'topic_id',
			'group_id',
			'low',
			'high',
		]);

		expect(line).toBe(
			'level=WARN msg="lease reclaimed from expired worker" code=VK0026 topic_id=1 group_id=7 low=4100 high=4200',
		);
	});

	it('keeps the minimal line for a code with no placeholder names', () => {
		const line = errorExampleLine(
			'topic name is required',
			'pass the topic name to RegisterTopic',
			'VK0001',
			[],
		);

		expect(line).toBe('topic name is required -- pass the topic name to RegisterTopic [VK0001]');
	});

	it('throws for a name with no example value', () => {
		expect(() => errorExampleLine('problem', null, 'VK9999', ['unregistered_name'])).toThrow(
			'no example value',
		);
	});
});
