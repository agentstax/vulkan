<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import FixLine from './fix-line.svelte';

	const { Story } = defineMeta({
		title: 'Board/FixLine',
		component: FixLine,
	});
</script>

<Story
	name="Static fix"
	args={{ fix: 'pass DestroyOptions.Force to destroy them with the topic', placeholders: [] }}
/>

<Story
	name="Three placeholders"
	args={{
		fix: 'migrate the {owner_kind} schema up from {version} to {build_version}',
		placeholders: ['owner_kind', 'version', 'build_version'],
	}}
/>

<!-- VK0013: the identifier's quotes belong to the fix, the value goes in raw -->
<Story
	name="Quoted identifier"
	args={{
		fix: 'register "{schedule}" with MessageAdmin.RegisterSchedule first',
		placeholders: ['schedule'],
	}}
/>
