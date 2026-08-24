<script lang="ts">
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import type { Consumer } from './types';

	type Props = {
		consumer: Consumer;
		ontick: () => void;
		onremove: () => void;
	};

	let { consumer, ontick, onremove }: Props = $props();
</script>

<div class="consumer-card">
	<div class="card-head">
		<span class="consumer-name">{consumer.name}</span>
		<span class="consumer-group">{consumer.group}</span>
	</div>
	<div class="card-out">
		{#each consumer.lines as line, index (index)}
			<div class="out-line" data-kind={line.kind}>
				<span class="out-text">{line.text}</span>
				{#if line.kind === 'handled'}
					<span class="out-status" data-status={line.status}>{line.status}</span>
				{/if}
			</div>
		{/each}
	</div>
	<div class="card-foot">
		<ChromeButton label="Tick ▸" tone="primary" disabled={false} onclick={ontick} />
		<ChromeButton label="Remove" tone="quiet" disabled={false} onclick={onremove} />
	</div>
</div>

<style src="./consumer-card.css"></style>
