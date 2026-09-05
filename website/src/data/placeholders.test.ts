import { describe, expect, it } from 'vitest';
import { codeRecords } from './codes';
import { filledSql } from '../components/highlighted-sql/highlight';
import { fillText, logAttributes } from '../helpers/placeholders';

// Placeholders are declared in Go and read here, so the two halves have to
// agree. These walk the real export rather than made-up records.

// every code that substitutes anything, and the names it substitutes
function declared(): { code: string; placeholders: string[] }[] {
	return Object.values(codeRecords)
		.filter((record) => record.kind === 'error' || record.kind === 'event')
		.map((record) => ({
			code: record.code,
			placeholders: [
				...new Set([
					...(record.queries ?? []).flatMap((query) => query.placeholders),
					...(record.kind === 'error' ? (record.fix_placeholders ?? []) : []),
				]),
			],
		}))
		.filter((entry) => entry.placeholders.length > 0);
}

// a value shaped like the one the attribute really carries: ids and versions
// are numbers, everything else a name
function sampleValue(placeholder: string): string {
	return placeholder.endsWith('_id') ||
		placeholder.endsWith('version') ||
		['low', 'high', 'attempt'].includes(placeholder)
		? '7'
		: 'orders';
}

describe('declared placeholders against the site that fills them', () => {
	it('declares at least the codes the library carries queries for', () => {
		expect(declared().length).toBeGreaterThanOrEqual(18);
	});

	it('reads every declared name back out of a log line', () => {
		const unreadable: string[] = [];

		for (const { code, placeholders } of declared()) {
			const line = placeholders
				.map((placeholder) => `${placeholder}=${sampleValue(placeholder)}`)
				.join(' ');
			const values = logAttributes(line, placeholders);
			for (const placeholder of placeholders) {
				if (!values.has(placeholder)) {
					unreadable.push(`${code} {${placeholder}}`);
				}
			}
		}
		expect(unreadable).toEqual([]);
	});

	// a blank no value can legally reach sits where SQL has no room for one
	it('leaves no blank in a query once every name is filled', () => {
		const unfilled: string[] = [];

		for (const record of Object.values(codeRecords)) {
			if (record.kind === 'metric' || record.kind === 'alert') {
				continue;
			}
			for (const query of record.queries ?? []) {
				const values = new Map(
					query.placeholders.map((placeholder) => [placeholder, sampleValue(placeholder)]),
				);
				if (filledSql(query.sql, values).includes('{')) {
					unfilled.push(`${record.code}: ${query.label}`);
				}
			}
		}
		expect(unfilled).toEqual([]);
	});

	it('leaves no blank in a fix once every name is filled', () => {
		const unfilled: string[] = [];

		for (const record of Object.values(codeRecords)) {
			if (record.kind !== 'error' || record.fix === undefined) {
				continue;
			}
			const values = new Map(
				(record.fix_placeholders ?? []).map((placeholder) => [
					placeholder,
					sampleValue(placeholder),
				]),
			);
			const remaining = fillText(record.fix, values).filter(
				(segment) => segment.kind === 'placeholder',
			);
			if (remaining.length > 0) {
				unfilled.push(`${record.code} ${remaining.map((segment) => segment.text).join(' ')}`);
			}
		}
		expect(unfilled).toEqual([]);
	});

	// the export saves this side a second parse, so it has to match the text
	it('exports the names each fix really substitutes', () => {
		const wrong: string[] = [];

		for (const record of Object.values(codeRecords)) {
			if (record.kind !== 'error') {
				continue;
			}
			const named = [...(record.fix ?? '').matchAll(/\{([a-z][a-z0-9_]*)\}/g)].map(
				(match) => match[1],
			);
			const exported = record.fix_placeholders ?? [];
			if ([...new Set(named)].join(',') !== exported.join(',')) {
				wrong.push(
					`${record.code}: text names ${named.join(',')}, export says ${exported.join(',')}`,
				);
			}
		}
		expect(wrong).toEqual([]);
	});
});
