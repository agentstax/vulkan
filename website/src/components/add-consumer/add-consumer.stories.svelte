<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import AddConsumer from './add-consumer.svelte';

	const { Story } = defineMeta({
		title: 'Board/AddConsumer',
		component: AddConsumer,
		args: {
			groups: ['billing', 'search'],
			errorMessage: null,
			disabled: false,
			onadd: () => {},
		},
	});
</script>

<Story name="Groups to join" />
<Story name="No groups yet" args={{ groups: [] }} />
<Story name="Registering" args={{ disabled: true }} />
<Story name="Register failed" args={{ errorMessage: 'could not reach the database' }} />
<Story
	name="Many groups"
	args={{ groups: ['billing', 'search', 'audit', 'shipping', 'analytics'] }}
/>
