<script lang="ts">
	import ConsumerCard from '../consumer-card/consumer-card.svelte';
	import type { Consumer } from '../consumer-card/types';

	type Props = {
		consumers: Consumer[];
		ontick: (name: string) => void;
		onremove: (name: string) => void;
	};

	let { consumers, ontick, onremove }: Props = $props();
</script>

<div class="consumer-grid">
	{#each consumers as consumer (consumer.name)}
		<ConsumerCard
			{consumer}
			ontick={() => ontick(consumer.name)}
			onremove={() => onremove(consumer.name)}
		/>
	{/each}
</div>

<style src="./consumer-grid.css"></style>
