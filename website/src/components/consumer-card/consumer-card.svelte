<script lang="ts">
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import type { Consumer } from './types';

	type Props = {
		consumer: Consumer;
		disabled: boolean;
		onautorun: (on: boolean) => void;
		onremove: () => void;
	};

	let { consumer, disabled, onautorun, onremove }: Props = $props();
</script>

<div class="consumer-card">
	<div class="card-head">
		<span class="consumer-name">{consumer.name}</span>
		<span class="consumer-group">({consumer.group})</span>
		<ChromeButton
			label="auto-run"
			ariaLabel="Auto-run {consumer.name}"
			tone="primary"
			pressed={consumer.autoRun}
			{disabled}
			onclick={() => onautorun(!consumer.autoRun)}
		/>
		<ChromeButton
			label="✕"
			ariaLabel="Remove {consumer.name}"
			tone="close"
			pressed={null}
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
