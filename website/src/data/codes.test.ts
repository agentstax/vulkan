import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { codeRecords, type CodeRecord } from './codes';

const pagesDirectory = join(import.meta.dirname, '../content/docs/errors');

// Error pages are hand-written and stay that way, so their frontmatter
// repeats what the declaration says. These walks are what keep the repetition
// honest: a reworded problem line, fix, or recovery fails here until its page
// is updated in the same change.
describe('error pages against the declarations', () => {
	// codes.ts reads the export as a union on kind. A kind the union does not
	// name would be typed as something it is not, so the walk proves it first.
	it('declares every code as one of the four kinds', () => {
		const unknown = Object.values(codeRecords)
			.filter((record) => !['error', 'event', 'metric', 'alert'].includes(record.kind))
			.map((record) => `${record.code} is kind ${record.kind}`);
		expect(unknown).toEqual([]);
	});

	it('gives every declared code a page', () => {
		const missing = Object.keys(codeRecords).filter((code) => !pages().has(code));
		expect(missing).toEqual([]);
	});

	it('gives every page a declaration', () => {
		const orphaned = [...pages().keys()].filter((code) => codeRecords[code] === undefined);
		expect(orphaned).toEqual([]);
	});

	it('titles each page with the declaration text', () => {
		for (const [code, frontmatter] of pages()) {
			const record = codeRecords[code];
			if (record === undefined) {
				continue;
			}
			expect(frontmatter['title'], `${code} title`).toBe(expectedTitle(record));
		}
	});

	it('repeats the declared fix and recovery verbatim', () => {
		for (const [code, frontmatter] of pages()) {
			const record = codeRecords[code];
			if (record?.kind !== 'error') {
				continue;
			}
			expect(frontmatter['recovery'], `${code} recovery`).toBe(record.recovery);
			// a declaration with no fix leaves the page's field out too
			expect(frontmatter['fix'], `${code} fix`).toBe(record.fix);
		}
	});

	it('states the declaration kind each page documents', () => {
		for (const [code, frontmatter] of pages()) {
			const record = codeRecords[code];
			if (record === undefined) {
				continue;
			}
			expect(frontmatter['kind'], `${code} kind`).toBe(record.kind);
		}
	});
});

// An event's declared message is the static clause plus its consequence
// ("message dead-lettered -- unrecoverable, will not be retried"); the page
// is titled with the clause alone. A metric's or an alert's title is its
// name.
function expectedTitle(record: CodeRecord): string | undefined {
	switch (record.kind) {
		case 'error':
			return record.problem;
		case 'event':
			return record.message.split(' -- ')[0];
		case 'metric':
		case 'alert':
			return record.name;
	}
}

// pages reads each error page's frontmatter. The fields are one-line
// `key: "value"` pairs, so the site parses them here rather than carrying a
// YAML dependency for six keys. A fix that quotes an identifier
// (`register "{schedule}" with ...`) is written as a single-quoted scalar,
// which is why both quote characters open a value.
function pages(): Map<string, Record<string, string>> {
	const found = new Map<string, Record<string, string>>();

	for (const file of readdirSync(pagesDirectory)) {
		if (!file.endsWith('.md')) {
			continue;
		}
		const source = readFileSync(join(pagesDirectory, file), 'utf8');
		const frontmatter: Record<string, string> = {};
		for (const line of source.split('---')[1]?.split('\n') ?? []) {
			const match = /^([a-z_]+):\s*"(.*)"$|^([a-z_]+):\s*'(.*)'$/.exec(line);
			const key = match?.[1] ?? match?.[3];
			const value = match?.[2] ?? match?.[4];
			if (key !== undefined && value !== undefined) {
				frontmatter[key] = value;
			}
		}
		found.set(file.replace(/\.md$/, ''), frontmatter);
	}
	return found;
}
