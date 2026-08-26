import type { Preview } from '@storybook/svelte-vite';
import '../src/styles/global.css';
import { boardStyles } from '../src/state/board-style.svelte';

// Every component has as many looks as the board has styles, and a story is
// the component's done-checklist -- so the style is a toolbar global rather
// than a story per component per style. The toolbar sets the same one
// attribute the footer's dropdown does.
const preview: Preview = {
	globalTypes: {
		boardStyle: {
			description: 'Board style',
			toolbar: {
				title: 'Board style',
				icon: 'paintbrush',
				items: boardStyles.map((style) => ({ value: style.id, title: style.label })),
				dynamicTitle: true,
			},
		},
	},
	initialGlobals: {
		boardStyle: 'classic',
	},
	decorators: [
		(story, context) => {
			document.documentElement.dataset['boardStyle'] = String(context.globals['boardStyle']);
			return story();
		},
	],
};

export default preview;
