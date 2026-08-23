// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import svelte from '@astrojs/svelte';

// https://astro.build/config
export default defineConfig({
	site: 'https://vulkan-5ss.pages.dev',
	vite: {
		// PGlite locates its wasm assets itself; pre-bundling breaks the paths
		optimizeDeps: { exclude: ['@electric-sql/pglite'] },
	},
	markdown: {
		shikiConfig: {
			// the board's code dialect: keywords in the console's SQL-keyword
			// blue, strings one quiet red, everything else ink -- values mirror
			// the global.css tokens (band-blue-end, sticky-label-red, ink,
			// console-sql-pale, ink-faint)
			theme: {
				name: 'vulkan-board',
				type: 'light',
				colors: {
					'editor.background': '#f6f9fb',
					'editor.foreground': '#22303c',
				},
				settings: [
					{ settings: { foreground: '#22303c' } },
					{
						// named keyword families only -- keyword.operator stays ink
						scope: [
							'keyword.control',
							'keyword.function',
							'keyword.package',
							'keyword.import',
							'keyword.type',
							'keyword.var',
							'keyword.const',
							'keyword.struct',
							'keyword.interface',
							'keyword.map',
							'keyword.channel',
							'keyword.other',
							'storage',
							'constant.language',
						],
						settings: { foreground: '#184e7c', fontStyle: 'bold' },
					},
					{ scope: ['string'], settings: { foreground: '#b03a2e' } },
					{ scope: ['comment'], settings: { foreground: '#7c8b98', fontStyle: 'italic' } },
				],
			},
		},
	},
	integrations: [
		svelte(),
		starlight({
			title: 'Vulkan',
			// code fences render through plain Shiki + the board theme above;
			// expressive-code would override it and leaves with Starlight anyway
			expressiveCode: false,
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
