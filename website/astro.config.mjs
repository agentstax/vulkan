// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import svelte from '@astrojs/svelte';

// https://astro.build/config
export default defineConfig({
	site: 'https://vulkan-5ss.pages.dev',
	integrations: [
		svelte(),
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
						{
							label: 'The Demo: Try to Lose a Message',
							slug: 'demo',
							badge: { text: 'Proposed', variant: 'caution' },
						},
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
						{ label: 'Fan-out, Retention & Replay', slug: 'concepts/fan-out' },
						{ label: 'Routing', slug: 'concepts/routing' },
						{ label: 'Ordering & Concurrency', slug: 'concepts/ordering' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Transactional Produce', slug: 'guides/transactional-produce' },
						{
							label: 'Replaying History',
							slug: 'guides/replay',
							badge: { text: 'Partly proposed', variant: 'caution' },
						},
						{ label: 'Dead Letters & Recovery', slug: 'guides/dead-letters' },
						{ label: 'Upgrades & Migrations', slug: 'guides/migrations' },
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
				{
					label: 'Errors',
					collapsed: true,
					items: [{ autogenerate: { directory: 'errors' } }],
				},
				{ label: 'Roadmap', slug: 'roadmap' },
			],
		}),
	],
});
