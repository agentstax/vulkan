<script lang="ts">
	import ConsumerCard from '../consumer-card/consumer-card.svelte';
	import type { Consumer } from '../consumer-card/types';

	type Props = {
		consumers: Consumer[];
		disabled: boolean;
		onautorun: (name: string, on: boolean) => void;
		onremove: (name: string) => void;
	};

	let { consumers, disabled, onautorun, onremove }: Props = $props();
</script>

<div class="consumer-grid">
	{#each consumers as consumer (consumer.name)}
		<ConsumerCard
			{consumer}
			{disabled}
			onautorun={(on) => onautorun(consumer.name, on)}
			onremove={() => onremove(consumer.name)}
		/>
	{/each}
</div>

<style src="./consumer-grid.css"></style>
