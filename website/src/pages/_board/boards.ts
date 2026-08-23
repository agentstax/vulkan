export type Board = {
	title: string;
	href: string;
	description: string;
	contains: (id: string) => boolean;
};

export const boards: Board[] = [
	{
		title: 'Getting Started',
		href: '/quickstart/',
		description: 'install, migrate init, first produce and consume',
		contains: (id) => ['quickstart', 'why-vulkan', 'demo', 'cloud'].includes(id),
	},
	{
		title: 'Concepts',
		href: '/concepts/queue-and-log/',
		description: 'queue & log, lifecycle, ordering, routing, fan-out, architecture',
		contains: (id) => id.startsWith('concepts/'),
	},
	{
		title: 'Guides',
		href: '/guides/transactional-produce/',
		description: 'transactional produce, dead letters, replay, migrations',
		contains: (id) => id.startsWith('guides/'),
	},
	{
		title: 'Troubleshooting',
		href: '/errors/',
		description: 'every VK error code and log event, one thread each',
		contains: (id) => id.startsWith('errors/') && id !== 'errors/index',
	},
	{
		title: 'Compare',
		href: '/compare/kafka/',
		description: 'Kafka · RabbitMQ & SQS — shipped behavior only, no wishful checkmarks',
		contains: (id) => id.startsWith('compare/'),
	},
];

export const stickyIds = ['quickstart', 'why-vulkan'];
