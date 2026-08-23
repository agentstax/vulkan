import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { sveltePreprocess } from 'svelte-preprocess';

// sveltePreprocess inlines <style src="./x.css"> before the compiler,
// so sibling css files are scoped exactly like inline styles; script
// transforms stay with vitePreprocess (typescript: false avoids a
// second TS pipeline).
export default {
	preprocess: [sveltePreprocess({ typescript: false }), vitePreprocess()],
};
