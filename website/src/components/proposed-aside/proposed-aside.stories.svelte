<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import ProposedAside from './proposed-aside.svelte';

	const { Story } = defineMeta({
		title: 'Board/ProposedAside',
		component: ProposedAside,
		args: { title: 'Strict per-key FIFO' },
	});
</script>

<Story name="Aside">
	{#snippet template(args)}
		<div class="thread-body">
			<ProposedAside {...args}>
				<p>
					"Every message with this key, in order, one at a time" — a separate partition key
					beside the compaction key — is a designed-but-unbuilt proposal. If your workload needs
					it, today Vulkan doesn't ship it.
				</p>
			</ProposedAside>
		</div>
	{/snippet}
</Story>
