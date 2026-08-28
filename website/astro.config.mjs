// @ts-check
import { defineConfig } from 'astro/config';
import { unified } from '@astrojs/markdown-remark';
import mdx from '@astrojs/mdx';
import svelte from '@astrojs/svelte';
import { remarkDecisionRecords } from './src/helpers/decision-records.ts';
import { siteUrl } from './src/site.ts';

// named keyword families only -- keyword.operator stays ink
const keywordScopes = [
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
];

// https://astro.build/config
export default defineConfig({
	site: siteUrl,
	vite: {
		build: {
			// Vite 8's baseline-widely-available list, pinned -- the default
			// floats per Vite major, so the floor moves only by deliberate edit
			target: ['chrome111', 'edge111', 'firefox114', 'safari16.4', 'ios16.4'],
		},
		// PGlite locates its wasm assets itself; pre-bundling breaks the paths
		optimizeDeps: { exclude: ['@electric-sql/pglite'] },
	},
	markdown: {
		processor: unified({ remarkPlugins: [remarkDecisionRecords] }),
		shikiConfig: {
			// The board's code dialect: keywords in the console's SQL-keyword
			// colour, strings one quiet red, everything else ink -- each theme's
			// values mirror the global.css tokens of the board style it is named
			// for. Both ship on every fence; defaultColor off means neither is
			// baked in as an inline colour -- Shiki writes --shiki-light and
			// --shiki-dark per token and the base layer picks the side, so a
			// reader switching styles switches the code with the page.
			themes: {
				light: {
					name: 'vulkan-board',
					type: 'light',
					// console-sql-pale, ink
					colors: {
						'editor.background': '#f6f9fb',
						'editor.foreground': '#22303c',
					},
					settings: [
						{ settings: { foreground: '#22303c' } },
						// band-blue-end
						{ scope: keywordScopes, settings: { foreground: '#184e7c', fontStyle: 'bold' } },
						// sticky-label-red
						{ scope: ['string'], settings: { foreground: '#b03a2e' } },
						// ink-faint
						{ scope: ['comment'], settings: { foreground: '#7c8b98', fontStyle: 'italic' } },
					],
				},
				dark: {
					name: 'vulkan-board-night',
					type: 'dark',
					// console-sql-pitch, ink-silver
					colors: {
						'editor.background': '#12161b',
						'editor.foreground': '#c8ccd2',
					},
					settings: [
						{ settings: { foreground: '#c8ccd2' } },
						// console-keyword-sky
						{ scope: keywordScopes, settings: { foreground: '#7fb3dd', fontStyle: 'bold' } },
						// sticky-label-coral
						{ scope: ['string'], settings: { foreground: '#e0685a' } },
						// ink-silver-faint
						{ scope: ['comment'], settings: { foreground: '#6b7178', fontStyle: 'italic' } },
					],
				},
			},
			defaultColor: false,
		},
	},
	integrations: [svelte(), mdx()],
});
