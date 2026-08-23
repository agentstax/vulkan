import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import ts from 'typescript-eslint';

export default ts.config(
	{ ignores: ['dist/', '.astro/', 'node_modules/', 'storybook-static/'] },
	js.configs.recommended,
	...ts.configs.recommended,
	...svelte.configs.recommended,
	{
		files: ['**/*.svelte'],
		languageOptions: { parserOptions: { parser: ts.parser } },
	},
	{
		rules: {
			'@typescript-eslint/no-explicit-any': 'error',
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
		files: ['.storybook/preview.ts'],
		rules: { 'no-restricted-imports': 'off' },
	},
);
