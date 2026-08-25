<script lang="ts">
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import type { Consumer } from './types';

	type Props = {
		consumer: Consumer;
		disabled: boolean;
		ontick: () => void;
		onremove: () => void;
	};

	let { consumer, disabled, ontick, onremove }: Props = $props();
</script>

<div class="consumer-card">
	<div class="card-head">
		<span class="consumer-name">{consumer.name}</span>
		<span class="consumer-group">({consumer.group})</span>
		<ChromeButton
			label="Run ▸"
			ariaLabel="Run {consumer.name}"
			tone="primary"
			{disabled}
			onclick={ontick}
		/>
		<ChromeButton
			label="✕"
			ariaLabel="Remove {consumer.name}"
			tone="close"
			{disabled}
			onclick={onremove}
		/>
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
	<div class="card-status" data-tone={consumer.status.tone}>{consumer.status.text}</div>
</div>

<style src="./consumer-card.css"></style>
