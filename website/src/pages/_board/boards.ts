import type { JumpTarget } from '../../components/jump-to/types';

export type Board = {
	title: string;
	// the path segment of the board's listing page: /boards/<slug>/
	slug: string;
	description: string;
	// the board's threads in reading order, chosen from the site's page ids
	threads: (ids: string[]) => string[];
};

export const boards: Board[] = [
	{
		title: 'Getting Started',
		slug: 'getting-started',
		description: 'install, migrate init, first produce and consume — and where Vulkan is going',
		threads: () => ['why-vulkan', 'demo', 'quickstart', 'roadmap'],
	},
	{
		title: 'Concepts',
		slug: 'concepts',
		description:
			'queue & log, lifecycle, message key, ordering, routing, fan-out, architecture, table design',
		threads: () => [
			'concepts/queue-and-log',
			'concepts/architecture',
			'concepts/table-design',
			'concepts/lifecycle',
			'concepts/fan-out',
			'concepts/routing',
			'concepts/message-key',
			'concepts/ordering',
		],
	},
	{
		title: 'Guides',
		slug: 'guides',
		description:
			'transactional produce, side effects & retries, dead letters, consumer timeouts, replay, migrations',
		threads: () => [
			'guides/transactional-produce',
			'guides/side-effects-and-retries',
			'guides/replay',
			'guides/dead-letters',
			'guides/handler-outcomes',
			'guides/new-group-start',
			'guides/ordered-delivery',
			'guides/consumer-timeouts',
			'guides/schema-versions',
			'guides/migrations',
		],
	},
	{
		title: 'Troubleshooting',
		slug: 'troubleshooting',
		description: 'every VK error code and log event, one thread each',
		// the code index leads, then the code threads in code order
		threads: (ids) => ['errors', ...ids.filter(isErrorThread).sort()],
	},
	{
		title: 'Compare',
		slug: 'compare',
		description: 'Kafka · RabbitMQ & SQS — shipped behavior only, no wishful checkmarks',
		threads: () => ['compare/kafka', 'compare/rabbitmq-sqs', 'compare/job-queues'],
	},
	{
		title: 'Decision records',
		slug: 'decisions',
		description: 'the why behind shipped behavior — every settled design decision, append-only',
		// the record index leads, then the record threads in number order
		threads: (ids) => ['decisions', ...ids.filter(isDecisionRecordThread).sort()],
	},
];

export const stickyIds = ['quickstart', 'why-vulkan'];

// the Jump to select navigates to each board's listing page
export const jumpTargets: JumpTarget[] = boards.map((board) => ({
	label: board.title,
	href: boardHref(board),
}));

export function boardHref(board: Board): string {
	return `/boards/${board.slug}/`;
}

export function isErrorThread(id: string): boolean {
	return id.startsWith('errors/');
}

export function threadCode(id: string): string {
	const code = id.split('/')[1];
	if (code === undefined) {
		throw new Error(`thread "${id}" carries no code segment`);
	}
	return code;
}

export function isDecisionRecordThread(id: string): boolean {
	return id.startsWith('decisions/');
}

export function recordNumber(id: string): string {
	const number = id.split('/')[1];
	if (number === undefined) {
		throw new Error(`thread "${id}" carries no record number segment`);
	}
	return number;
}
