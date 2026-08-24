<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import AddConsumer from './add-consumer.svelte';

	const { Story } = defineMeta({
		title: 'Board/AddConsumer',
		component: AddConsumer,
		args: {
			groups: ['billing', 'search'],
			onadd: () => {},
		},
	});
</script>

<Story name="Groups to join" />
<Story name="No groups yet" args={{ groups: [] }} />
<Story
	name="Many groups"
	args={{ groups: ['billing', 'search', 'audit', 'shipping', 'analytics'] }}
/>
