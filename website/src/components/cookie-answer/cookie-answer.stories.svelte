<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import CookieAnswer from './cookie-answer.svelte';

	// the real copy lives in cookie-notice's answers table and is storied
	// there; these two cover what this component itself can render
	const { Story } = defineMeta({
		title: 'Board/CookieAnswer',
		component: CookieAnswer,
		args: {
			answer: {
				body: 'the answer the pressed control had for you.',
				acceptLabel: 'the label that leads somewhere',
				acceptLink: 'https://example.com',
				dismissLabel: 'the label that closes this',
			},
			titleId: 'cookie-answer-body',
			onDismiss: () => {},
		},
	});
</script>

<Story name="With a link" />
<Story
	name="Dismiss only"
	args={{
		answer: {
			body: 'the answer that asks the reader for nothing.',
			acceptLabel: null,
			acceptLink: null,
			dismissLabel: 'the label that closes this',
		},
	}}
/>
