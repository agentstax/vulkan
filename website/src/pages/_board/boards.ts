export type Board = {
	title: string;
	href: string;
	description: string;
	// the board's threads in reading order, chosen from the site's page ids
	threads: (ids: string[]) => string[];
};

export const boards: Board[] = [
	{
		title: 'Getting Started',
		href: '/quickstart/',
		description: 'install, migrate init, first produce and consume',
		threads: () => ['why-vulkan', 'demo', 'quickstart', 'cloud'],
	},
	{
		title: 'Concepts',
		href: '/concepts/queue-and-log/',
		description: 'queue & log, lifecycle, ordering, routing, fan-out, architecture',
		threads: () => [
			'concepts/queue-and-log',
			'concepts/architecture',
			'concepts/lifecycle',
			'concepts/fan-out',
			'concepts/routing',
			'concepts/ordering',
		],
	},
	{
		title: 'Guides',
		href: '/guides/transactional-produce/',
		description: 'transactional produce, dead letters, replay, migrations',
		threads: () => [
			'guides/transactional-produce',
			'guides/replay',
			'guides/dead-letters',
			'guides/migrations',
		],
	},
	{
		title: 'Troubleshooting',
		href: '/errors/',
		description: 'every VK error code and log event, one thread each',
		// error threads read in code order
		threads: (ids) => ids.filter(isErrorThread).sort(),
	},
	{
		title: 'Compare',
		href: '/compare/kafka/',
		description: 'Kafka · RabbitMQ & SQS — shipped behavior only, no wishful checkmarks',
		threads: () => ['compare/kafka', 'compare/rabbitmq-sqs', 'compare/job-queues'],
	},
];

export const stickyIds = ['quickstart', 'why-vulkan'];

export function isErrorThread(id: string): boolean {
	return id.startsWith('errors/') && id !== 'errors/index';
}

export function threadCode(id: string): string {
	const code = id.split('/')[1];
	if (code === undefined) {
		throw new Error(`thread "${id}" carries no code segment`);
	}
	return code;
}
