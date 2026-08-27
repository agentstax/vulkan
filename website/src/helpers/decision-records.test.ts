import { describe, expect, it } from 'vitest';
import { recordTitle, transformDecisionRecord, type MarkdownNode } from './decision-records';

describe('recordTitle', () => {
	it('strips the number from an em-dash H1', () => {
		const body = '---\nstatus: accepted\n---\n\n# 0590 — a fix substitutes values\n\n## Context\n';
		expect(recordTitle('0590', body)).toBe('a fix substitutes values');
	});

	it('strips the number from a double-hyphen H1', () => {
		expect(recordTitle('0560', '# 0560 -- SQL literal owner comments\n')).toBe(
			'SQL literal owner comments',
		);
	});

	it('keeps a bare H1 whole', () => {
		expect(recordTitle('0538', '# File content ordering convention\n')).toBe(
			'File content ordering convention',
		);
	});

	it('throws when no H1 exists', () => {
		expect(() => recordTitle('0590', '## Context only\n')).toThrow('carries no H1 title');
	});
});

describe('transformDecisionRecord', () => {
	const recordNumbers = new Set(['0555', '0590']);

	it('removes the leading H1', () => {
		const tree: MarkdownNode = {
			type: 'root',
			children: [
				{ type: 'heading', depth: 1, children: [{ type: 'text', value: '0590 — a title' }] },
				{ type: 'heading', depth: 2, children: [{ type: 'text', value: 'Context' }] },
			],
		};

		transformDecisionRecord(tree, recordNumbers);

		expect(tree.children).toHaveLength(1);
		expect(tree.children?.[0]?.depth).toBe(2);
	});

	it('links a citation whose record exists and leaves the rest literal', () => {
		const tree: MarkdownNode = {
			type: 'root',
			children: [
				{ type: 'paragraph', children: [{ type: 'text', value: 'see [0555] and [0001].' }] },
			],
		};

		transformDecisionRecord(tree, recordNumbers);

		expect(tree.children?.[0]?.children).toEqual([
			{ type: 'text', value: 'see ' },
			{ type: 'link', url: '/decisions/0555/', children: [{ type: 'text', value: '[0555]' }] },
			{ type: 'text', value: ' and [0001].' },
		]);
	});

	it('leaves a citation inside an existing link as written', () => {
		const link: MarkdownNode = {
			type: 'link',
			url: '/elsewhere/',
			children: [{ type: 'text', value: '[0590]' }],
		};
		const tree: MarkdownNode = {
			type: 'root',
			children: [{ type: 'paragraph', children: [link] }],
		};

		transformDecisionRecord(tree, recordNumbers);

		expect(tree.children?.[0]?.children).toEqual([link]);
	});

	it('reaches citations nested below the paragraph level', () => {
		const tree: MarkdownNode = {
			type: 'root',
			children: [
				{
					type: 'listItem',
					children: [{ type: 'paragraph', children: [{ type: 'text', value: '[0590]' }] }],
				},
			],
		};

		transformDecisionRecord(tree, recordNumbers);

		expect(tree.children?.[0]?.children?.[0]?.children?.[0]?.type).toBe('link');
	});
});
