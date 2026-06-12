// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://vulkan-5ss.pages.dev',
	integrations: [
		starlight({
			title: 'Vulkan',
			description:
				'The message platform forged in Postgres. Queue, event log, and router in one system — open source, with a fully managed cloud.',
			logo: {
				src: './src/assets/logo.svg',
				alt: 'Vulkan',
			},
			social: [
				{
					icon: 'github',
					label: 'GitHub',
					href: 'https://github.com/agentstax/vulkan',
				},
			],
			customCss: ['./src/styles/custom.css'],
			sidebar: [
				{
					label: 'Start Here',
					items: [
						{ label: 'Why Vulkan', slug: 'why-vulkan' },
						{ label: 'The Demo: Try to Lose a Message', slug: 'demo' },
						{ label: 'Quickstart', slug: 'quickstart' },
						{ label: 'Vulkan Cloud', slug: 'cloud' },
					],
				},
				{
					label: 'Concepts',
					items: [
						{ label: 'Queues, Logs & the Fusion', slug: 'concepts/queue-and-log' },
						{ label: 'Architecture', slug: 'concepts/architecture' },
						{ label: 'Message Lifecycle', slug: 'concepts/lifecycle' },
						{ label: 'Streams, Replay & Fan-out', slug: 'concepts/streams' },
						{ label: 'Routing', slug: 'concepts/routing' },
						{ label: 'Ordering & FIFO', slug: 'concepts/ordering' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Transactional Enqueue', slug: 'guides/transactional-enqueue' },
						{ label: 'Replaying History', slug: 'guides/replay' },
						{ label: 'Dead Letters & Recovery', slug: 'guides/dead-letters' },
					],
				},
				{
					label: 'Compare',
					items: [
						{ label: 'Vulkan vs. Kafka', slug: 'compare/kafka' },
						{ label: 'Vulkan vs. RabbitMQ & SQS', slug: 'compare/rabbitmq-sqs' },
						{ label: 'Vulkan vs. Job Queues', slug: 'compare/job-queues' },
					],
				},
				{ label: 'Roadmap', slug: 'roadmap' },
			],
		}),
	],
});
