import type { StorybookConfig } from '@storybook/svelte-vite';

const config: StorybookConfig = {
	framework: '@storybook/svelte-vite',
	stories: ['../src/components/**/*.stories.svelte'],
	addons: ['@storybook/addon-svelte-csf'],
	// An Astro project has no vite.config.ts, so nothing supplies
	// vite-plugin-svelte to Storybook's builder; prepend it so .svelte
	// files compile before the plugins that read compiled output.
	viteFinal: async (viteConfig) => {
		const { svelte } = await import('@sveltejs/vite-plugin-svelte');
		return { ...viteConfig, plugins: [svelte(), ...(viteConfig.plugins ?? [])] };
	},
};

export default config;
