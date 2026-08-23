// @ts-check
import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import svelte from '@astrojs/svelte';

// https://astro.build/config
export default defineConfig({
	site: 'https://vulkan-5ss.pages.dev',
	vite: {
		// PGlite locates its wasm assets itself; pre-bundling breaks the paths
		optimizeDeps: { exclude: ['@electric-sql/pglite'] },
		build: {
			rollupOptions: {
				output: {
					// without this, Vite writes its module-preload helper into the
					// console island's chunk; the PGlite chunk then imports that
					// chunk for the helper alone, and the preload list names the
					// island's css by its pre-merge name -- a file Astro never
					// emits, so the console's first Run 404s and fails
					manualChunks: (id) => (id.includes('vite/preload-helper') ? 'preload-helper' : undefined),
				},
			},
		},
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
	integrations: [svelte(), mdx()],
});
