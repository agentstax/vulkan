<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ChromeButton from './chrome-button.svelte';

	const { Story } = defineMeta({
		title: 'Board/ChromeButton',
		component: ChromeButton,
		args: {
			label: 'Run ▸',
			ariaLabel: 'Run the query',
			tone: 'primary',
			disabled: false,
			onclick: () => {},
		},
	});
</script>

<Story name="Enabled" />
<Story name="Disabled" args={{ disabled: true }} />
<Story name="Quiet" args={{ label: 'Add', ariaLabel: 'Add a consumer', tone: 'quiet' }} />
<Story
	name="Quiet disabled"
	args={{ label: 'Add', ariaLabel: 'Add a consumer', tone: 'quiet', disabled: true }}
/>
<Story name="Close box" args={{ label: '✕', ariaLabel: 'Remove consumer 1', tone: 'close' }} />
<Story
	name="Close box disabled"
	args={{ label: '✕', ariaLabel: 'Remove consumer 1', tone: 'close', disabled: true }}
/>
