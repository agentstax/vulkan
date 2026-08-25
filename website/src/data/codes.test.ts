import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import codes from './codes.json';

type CodeRecord = {
	code: string;
	kind: string;
	problem?: string;
	recovery?: string;
	fix?: string;
	message?: string;
	name?: string;
};

const declared = codes.codes as Record<string, CodeRecord>;
const pagesDirectory = join(import.meta.dirname, '../content/docs/errors');

// Error pages are hand-written and stay that way, so their frontmatter
// repeats what the declaration says. These walks are what keep the repetition
// honest: a reworded problem line, fix, or recovery fails here until its page
// is updated in the same change.
describe('error pages against the declarations', () => {
	it('gives every declared code a page', () => {
		const missing = Object.keys(declared).filter((code) => !pages().has(code));
		expect(missing).toEqual([]);
	});

	it('gives every page a declaration', () => {
		const orphaned = [...pages().keys()].filter((code) => declared[code] === undefined);
		expect(orphaned).toEqual([]);
	});

	it('titles each page with the declaration text', () => {
		for (const [code, frontmatter] of pages()) {
			const record = declared[code];
			if (record === undefined) {
				continue;
			}
			expect(frontmatter['title'], `${code} title`).toBe(expectedTitle(record));
		}
	});

	it('repeats the declared fix and recovery verbatim', () => {
		for (const [code, frontmatter] of pages()) {
			const record = declared[code];
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
			const record = declared[code];
			if (record === undefined) {
				continue;
			}
			expect(frontmatter['kind'], `${code} kind`).toBe(record.kind);
		}
	});
});

// An event's declared message is the static clause plus its consequence
// ("message dead-lettered -- unrecoverable, will not be retried"); the page
// is titled with the clause alone. A metric's title is its name.
function expectedTitle(record: CodeRecord): string | undefined {
	switch (record.kind) {
		case 'error':
			return record.problem;
		case 'metric':
			return record.name;
		default:
			return record.message?.split(' -- ')[0];
	}
}

// pages reads each error page's frontmatter. The fields are one-line
// `key: "value"` pairs, so the site parses them here rather than carrying a
// YAML dependency for six keys.
function pages(): Map<string, Record<string, string>> {
	const found = new Map<string, Record<string, string>>();

	for (const file of readdirSync(pagesDirectory)) {
		if (!file.endsWith('.md')) {
			continue;
		}
		const source = readFileSync(join(pagesDirectory, file), 'utf8');
		const frontmatter: Record<string, string> = {};
		for (const line of source.split('---')[1]?.split('\n') ?? []) {
			const match = /^([a-z_]+):\s*"(.*)"$/.exec(line);
			if (match?.[1] !== undefined && match[2] !== undefined) {
				frontmatter[match[1]] = match[2];
			}
		}
		found.set(file.replace(/\.md$/, ''), frontmatter);
	}
	return found;
}
