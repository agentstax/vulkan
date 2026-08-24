import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

export const collections = {
	docs: defineCollection({
		loader: glob({
			pattern: '**/*.{md,mdx}',
			base: './src/content/docs',
			// the default generateId slugifies (VK0005 -> vk0005); ids and the
			// URLs built from them keep the file path's own casing
			generateId: ({ entry }) => entry.replace(/\.(md|mdx)$/, ''),
		}),
		schema: z.object({
			title: z.string().describe('the thread title; the H1 the page renders under'),
			description: z
				.string()
				.optional()
				.describe('the meta description; title stands in when absent'),
			kind: z
				.enum(['error', 'event', 'metric'])
				.optional()
				.describe('what the VK code names; present on every error-board page, absent elsewhere'),
			recovery: z
				.enum(['permanent', 'transient'])
				.optional()
				.describe('whether an unchanged retry can succeed; error pages only'),
			level: z
				.enum(['info', 'warn', 'error'])
				.optional()
				.describe('the log level the event is emitted at; event pages only'),
			consequence: z
				.string()
				.optional()
				.describe('the consequence clause shown beside the code, verbatim from the declaration'),
			fix: z
				.string()
				.optional()
				.describe('the declared fix, verbatim; absent when the code cannot know one'),
		}),
	}),
};
