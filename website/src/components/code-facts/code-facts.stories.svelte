<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import CodeFacts from './code-facts.svelte';

	const { Story } = defineMeta({
		title: 'Board/CodeFacts',
		component: CodeFacts,
		args: {
			code: 'VK0005',
			classification: 'recovery permanent',
			consequence: 'an unchanged retry cannot succeed; retry machinery stops immediately',
		},
	});
</script>

<Story name="Error" />

<Story
	name="Event"
	args={{
		code: 'VK0028',
		classification: 'log event at warn',
		consequence:
			'degraded but self-healing, or a durable data consequence; an operator should learn of it eventually',
	}}
/>
