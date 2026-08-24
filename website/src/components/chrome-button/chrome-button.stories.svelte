<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ChromeButton from './chrome-button.svelte';

	const { Story } = defineMeta({
		title: 'Board/ChromeButton',
		component: ChromeButton,
		args: {
			label: 'Run ▸',
			tone: 'primary',
			disabled: false,
			onclick: () => {},
		},
	});
</script>

<Story name="Enabled" />
<Story name="Disabled" args={{ disabled: true }} />
<Story name="Quiet" args={{ label: 'Remove', tone: 'quiet' }} />
<Story name="Quiet disabled" args={{ label: 'Remove', tone: 'quiet', disabled: true }} />
