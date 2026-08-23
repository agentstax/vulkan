<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ThreadAside from './thread-aside.svelte';

	const { Story } = defineMeta({
		title: 'Board/ThreadAside',
		component: ThreadAside,
		args: { label: 'PROPOSED', title: 'Strict per-key FIFO' },
	});
</script>

<Story name="Proposed">
	{#snippet template(args)}
		<div class="thread-body">
			<ThreadAside {...args}>
				<p>
					"Every message with this key, in order, one at a time" — a separate partition key
					beside the compaction key — is a designed-but-unbuilt proposal. If your workload needs
					it, today Vulkan doesn't ship it.
				</p>
			</ThreadAside>
		</div>
	{/snippet}
</Story>

<Story name="Caution" args={{ label: 'CAUTION', title: 'At-least-once, and proud of it' }}>
	{#snippet template(args)}
		<div class="thread-body">
			<ThreadAside {...args}>
				<p>A crash after your handler succeeds but before the delivery is recorded means the
					message runs again — make handlers idempotent.</p>
			</ThreadAside>
		</div>
	{/snippet}
</Story>
