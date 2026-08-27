import { readdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

// the build runs from website/, so the decision records sit one level up
export const decisionRecordsDirectory = '../docs/decisions';

// the slice of mdast the two transforms below touch
export type MarkdownNode = {
	type: string;
	depth?: number;
	value?: string;
	url?: string;
	children?: MarkdownNode[];
};

// a record's H1 is its title, in one of three shapes -- `# NNNN — <title>`,
// `# NNNN -- <title>`, or bare `# <title>`; the number and dash come off so
// every caller composes its own uniform display
export function recordTitle(recordNumber: string, body: string | undefined): string {
	const heading = body?.match(/^# (.+)$/m)?.[1];
	if (heading === undefined) {
		throw new Error(`decision record "${recordNumber}" carries no H1 title`);
	}
	return heading.replace(/^\d{4} (— |-- )/, '');
}

// remarkDecisionRecords adapts a record's markdown for the site: the H1
// duplicates the thread title band so it is removed, and [NNNN] citations
// become links to the record they name. Other files pass through untouched.
export function remarkDecisionRecords(): (tree: MarkdownNode, file: { path?: string }) => void {
	const recordsRoot = resolve(decisionRecordsDirectory);
	const recordNumbers = new Set(
		readdirSync(decisionRecordsDirectory)
			.filter((name) => name.endsWith('.md'))
			.map((name) => name.slice(0, 4)),
	);

	return (tree, file) => {
		if (file.path === undefined || dirname(resolve(file.path)) !== recordsRoot) {
			return;
		}
		transformDecisionRecord(tree, recordNumbers);
	};
}

export function transformDecisionRecord(tree: MarkdownNode, recordNumbers: Set<string>): void {
	removeLeadingHeading(tree);
	linkCitations(tree, recordNumbers);
}

// ***************
// *** HELPERS ***
// ***************

function removeLeadingHeading(tree: MarkdownNode): void {
	const children = tree.children ?? [];
	const index = children.findIndex((node) => node.type === 'heading' && node.depth === 1);
	if (index !== -1) {
		children.splice(index, 1);
	}
}

function linkCitations(node: MarkdownNode, recordNumbers: Set<string>): void {
	// a citation already inside a link stays as written
	if (node.type === 'link' || node.children === undefined) {
		return;
	}

	node.children = node.children.flatMap((child) => {
		if (child.type !== 'text') {
			linkCitations(child, recordNumbers);
			return [child];
		}
		return splitCitations(child.value ?? '', recordNumbers);
	});
}

// splitCitations turns one text node into text and link nodes; a citation
// with no matching record renders literally (numbers were allocated with
// gaps)
function splitCitations(text: string, recordNumbers: Set<string>): MarkdownNode[] {
	const nodes: MarkdownNode[] = [];
	let consumed = 0;

	for (const match of text.matchAll(/\[(\d{4})\]/g)) {
		const number = match[1];
		if (number === undefined || !recordNumbers.has(number)) {
			continue;
		}

		if (match.index > consumed) {
			nodes.push({ type: 'text', value: text.slice(consumed, match.index) });
		}
		nodes.push({
			type: 'link',
			url: `/decisions/${number}/`,
			children: [{ type: 'text', value: match[0] }],
		});
		consumed = match.index + match[0].length;
	}

	if (consumed < text.length) {
		nodes.push({ type: 'text', value: text.slice(consumed) });
	}
	return nodes;
}
