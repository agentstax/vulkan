import { defineCollection, z } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

export const collections = {
	docs: defineCollection({
		loader: docsLoader(),
		schema: docsSchema({
			extend: z.object({
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
	}),
};
