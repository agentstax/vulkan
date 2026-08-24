import js from '@eslint/js';
import astro from 'eslint-plugin-astro';
import svelte from 'eslint-plugin-svelte';
import ts from 'typescript-eslint';

export default ts.config(
	{ ignores: ['dist/', '.astro/', 'node_modules/', 'storybook-static/'] },
	js.configs.recommended,
	...ts.configs.recommended,
	...svelte.configs.recommended,
	...astro.configs.recommended,
	{
		files: ['**/*.svelte', '**/*.svelte.ts'],
		languageOptions: { parserOptions: { parser: ts.parser } },
	},
	{
		// TypeScript already errors on unknown identifiers; eslint's no-undef
		// has no type awareness and false-flags browser globals and type names
		files: ['**/*.ts', '**/*.svelte', '**/*.svelte.ts'],
		rules: { 'no-undef': 'off' },
	},
	{
		rules: {
			'@typescript-eslint/no-explicit-any': 'error',
			// `{ children: _children, ...storyProps }` -- a story spreads its
			// args minus the snippet the template supplies itself
			'@typescript-eslint/no-unused-vars': ['error', { ignoreRestSiblings: true }],
			// a component css file reaches the page only through <style src>;
			// importing one makes it global css in disguise
			'no-restricted-imports': [
				'error',
				{
					patterns: [
						{
							group: ['*.css', '**/*.css'],
							message:
								'include component css via <style src="./<name>.css">; the global stylesheet is imported only by the layout',
						},
					],
				},
			],
		},
	},
	{
		// the sanctioned global-stylesheet import sites
		files: ['.storybook/preview.ts', 'src/layouts/BoardLayout.astro'],
		rules: { 'no-restricted-imports': 'off' },
	},
);
